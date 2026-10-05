#!/usr/bin/env python3
"""I16 synthetic engineering capacity profile; no production/pilot qualification."""
import argparse,collections,concurrent.futures,hashlib,http.server,json,math,os,platform,signal,ssl,sys,threading,time,urllib.parse,urllib.request
from pathlib import Path
if sys.flags.optimize:raise SystemExit("Qualification requires Python optimization disabled")
import integration as lab
import importlib.util
spec=importlib.util.spec_from_file_location('progress_probe',lab.ROOT/'scripts/progress-probe.py')
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module);probe=module.probe

def quantile(values,q):
    return sorted(values)[max(0,math.ceil(len(values)*q)-1)] if values else None

def metrics(base):
    with urllib.request.urlopen(base+'/metrics',timeout=5) as r:text=r.read().decode()
    out={}
    for line in text.splitlines():
        if line and not line.startswith('#'):
            name,value=line.rsplit(' ',1)
            out[name]=float(value)
    if out.get('regente_capacity_metrics_available')!=1:raise RuntimeError('Capacity metrics unavailable')
    return out

def cpu_seconds(proc):
    try:
        fields=Path('/proc/'+str(proc.pid)+'/stat').read_text().rsplit(')',1)[1].split()
        return (int(fields[11])+int(fields[12]))/os.sysconf('SC_CLK_TCK')
    except FileNotFoundError:return 0

def rss(proc):
    try:
        for line in Path('/proc/'+str(proc.pid)+'/status').read_text().splitlines():
            if line.startswith('VmRSS:'):return int(line.split()[1])*1024
    except FileNotFoundError:pass
    return 0

class Profile:
    def __init__(self,name,density,env,dsn,tls):
        self.name=name;self.env=env;self.density=density;self.directory=lab.RUN/name
        self.directory.mkdir();self.effects=self.directory/'effects.txt';self.lock=threading.Lock()
        self.external=http.server.ThreadingHTTPServer(('127.0.0.1',0),self.handler())
        self.thread=threading.Thread(target=self.external.serve_forever,daemon=True);self.thread.start()
        self.dbname='regente_capacity_'+name
        lab.command(lab.COMPOSE+['exec','-T','postgres','createdb','-U','regente',self.dbname])
        self.dsn=dsn.replace('/regente_lab?','/'+self.dbname+'?')
        self.key=self.directory/'audit.key';self.collector_token=self.directory/'collector-token'
        self.collector_token.write_text('capacity-independent-collector-fixture');self.collector_token.chmod(0o600)
        lab.command([str(lab.RUN/'seed'),'-db',self.dsn,'-audit-key',str(self.key),'-count',str(density)],env=dict(env,REGENTE_DISPOSABLE_DB='1'),timeout=600,name=name+'-seed')
        collector_addr='127.0.0.1:'+str(lab.free_port())
        self.journal=self.directory/'independent.jsonl'
        self.collector_args=[str(lab.RUN/'server'),'-addr',collector_addr,'-audit-collector-file',str(self.journal),'-audit-collector-public-key',str(self.key)+'.pub','-audit-token-file',str(self.collector_token),'-tls-cert',str(tls/'lab.crt'),'-tls-key',str(tls/'lab.key')]
        self.collector=lab.start_process(name+'-collector',self.collector_args,self.directory,env)
        self.defs=self.directory/'workspace/definitions/capacity';self.defs.mkdir(parents=True)
        prefix="printf '%%INSTANCEID\\n' >> '"+str(self.effects)+"'; "
        marker=self.directory/'retries';marker.mkdir()
        definitions=[
            ('fast','COMMAND',{'command':prefix+'echo completed'},{}),
            ('output','COMMAND',{'command':prefix+"head -c 65536 /dev/zero | tr '\\0' x"},{}),
            ('long','COMMAND',{'command':prefix+'sleep 3; echo completed'},{'agentId':'slow'}),
            ('retry','COMMAND',{'command':"if mkdir '"+str(marker)+"/%%INSTANCEID' 2>/dev/null; then echo retry-first; exit 1; fi; "+prefix+'echo completed'},{'retries':1}),
            ('http','HTTP',{'url':'http://127.0.0.1:'+str(self.external.server_port)+'/?instance=%%INSTANCEID'},{}),
            ('parent','COMMAND',{'command':prefix+'echo producer'},{'conditionsOutAdd':['CAPACITY_READY@stat']}),
            ('child','COMMAND',{'command':prefix+'echo consumer'},{'conditionsIn':['CAPACITY_READY@stat']}),
            ('probe','COMMAND',{'command':prefix+'echo canary'},{})]
        for name2,kind,params,extra in definitions:
            raw={'id':name2,'team':'capacity','label':'Synthetic '+name2,'jobType':kind,'environment':'capacity','schedule':{'enabled':False},'params':params,**extra}
            (self.defs/(name2+'.yaml')).write_text(json.dumps(raw))
        addr='127.0.0.1:'+str(lab.free_port());self.base='http://'+addr
        self.server_args=[str(lab.RUN/'server'),'-profile','production','-environment','capacity','-network-boundary','loopback','-control-plane-execution','deny','-app-url',self.base,'-addr',addr,'-db-driver','postgres','-db',self.dsn,'-audit-key',str(self.key),'-workspace',str(self.directory/'workspace'),'-api-token','','-server-agent=false','-selfmon=false','-tick-ms','2000','-audit-siem-url','https://'+collector_addr,'-audit-token-file',str(self.collector_token)]
        self.server=lab.start_process(name+'-server',self.server_args,self.directory,dict(env,REGENTE_BOOTSTRAP_PASSWORD='capacity-synthetic-password-32'))
        lab.eventually('capacity server',lambda:lab.request(self.base+'/health'))
        self.token=lab.request(self.base+'/api/auth/login','POST',{'username':'admin','password':'capacity-synthetic-password-32'},token='')['token']
        self.agents=[]
        for agent_id,slots in [('fast',4),('slow',1)]:
            issued=self.req('/api/agents/tokens','POST',{'agentId':agent_id,'environment':'capacity','capabilities':['COMMAND','HTTP','EXECUTION_V2'],'expiresAt':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime(time.time()+7200))})
            folder=self.directory/agent_id;folder.mkdir()
            args=[str(lab.RUN/'agent'),'-transport','v2','-server',self.base,'-id',agent_id,'-env','capacity','-caps','COMMAND,HTTP','-token',issued['token'],'-journal',str(folder/'journal.db'),'-concurrency',str(slots)]
            self.agents.append(lab.start_process(name+'-'+agent_id,args,folder,env))
        lab.eventually('agent capacity',lambda:metrics(self.base).get('regente_execution_agent_slots',0)==5)
        self.records=[];self.samples=[];self.request_times=[];self.request_errors=[];self.fault_interval=None

    def handler(self):
        owner=self
        class Effect(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                instance=urllib.parse.parse_qs(urllib.parse.urlsplit(self.path).query).get('instance',[''])[0]
                if not instance:self.send_error(400);return
                with owner.lock:
                    with owner.effects.open('a') as f:f.write(instance+'\n')
                self.send_response(200);self.end_headers();self.wfile.write(b'completed')
            def log_message(self,*args):pass
        return Effect

    def req(self,path,method='GET',data=None):
        return lab.request(self.base+path,method,data,token=self.token)

    def order(self,definition):
        begin=time.monotonic();offered=time.time()
        try:
            out=self.req('/api/definitions/'+definition+'/force','POST')
            return {'id':out['instanceId'],'definition':definition,'offeredAt':offered,'requestSeconds':time.monotonic()-begin}
        except Exception as error:return {'error':type(error).__name__,'definition':definition,'offeredAt':offered,'requestSeconds':time.monotonic()-begin}

    def inspect(self):
        sql="SELECT COALESCE(json_agg(row_to_json(t)),'[]'::json) FROM (SELECT i.id,i.definition_id,i.status,a.execution_id,a.attempt,a.state,a.created_at,a.accepted_at,a.started_at,a.finished_at,(SELECT ev.message FROM instance_events ev WHERE ev.instance_id=i.id AND ev.kind='eligible' ORDER BY ev.ts LIMIT 1) AS eligibility FROM instances i LEFT JOIN runtime_orders r ON r.instance_id=i.id LEFT JOIN execution_attempts a ON a.order_id=r.order_id WHERE i.definition_id!='retained' ORDER BY i.id,a.attempt) t"
        result=lab.command(lab.COMPOSE+['exec','-T','postgres','psql','-U','regente','-d',self.dbname,'-Atc',sql])
        return json.loads(result)

    def finished(self):
        rows=self.inspect()
        return rows and all(r['status']=='OK' and r['state'] in ('succeeded','failed') for r in rows)

    def exercise(self,rate,seconds,fault=False):
        # C1/C5: uma ordem filha permanece bloqueada até o produtor terminar.
        child=self.order('child');assert 'id' in child;self.records.append(child)
        time.sleep(2.5)
        assert self.req('/api/instances/'+child['id'])['status']=='WAITING','Condition gate bypassed'
        self.records.append(self.order('parent'))
        canary=probe(self.base,'probe',self.token,5)
        fault_proof=None
        start=time.monotonic();start_wall=time.time();next_offer=start;next_sample=start;index=0;futures=[];paused=False;resumed=False
        mix=('fast','output','long','retry','http','child','parent')
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
            while time.monotonic()-start<seconds:
                now=time.monotonic();elapsed=now-start
                if now>=next_offer:
                    futures.append(pool.submit(self.order,mix[index%len(mix)]));index+=1
                    multiplier=4 if elapsed%300<60 else 1
                    next_offer+=1/(rate*multiplier)
                    if now-next_offer>5:raise RuntimeError('Load producer could not sustain offered rate')
                if now>=next_sample:
                    m=metrics(self.base)
                    self.samples.append({'seconds':elapsed,'serverRSS':rss(self.server),'agentRSS':[rss(p) for p in self.agents],'serverCPUSeconds':cpu_seconds(self.server),'agentCPUSeconds':[cpu_seconds(p) for p in self.agents],'metrics':m})
                    # Operador concorrente: consulta autenticada durante as admissões.
                    self.req('/api/resources')
                    next_sample=now+5
                if fault and elapsed>seconds/2 and not paused:
                    for proc in self.agents:os.kill(proc.pid,signal.SIGSTOP)
                    paused=True;pause_at=time.monotonic();pause_wall=time.time()
                    assert self.req('/readyz')['ready'] is True
                    try:probe(self.base,'probe',self.token,5);raise AssertionError('Probe falsely passed while all agents were stopped')
                    except RuntimeError as error:
                        fault_proof={'healthReady':True,'stalledProgressDetected':True,'error':str(error),'pausedSeconds':None}
                    for proc in self.agents:os.kill(proc.pid,signal.SIGCONT)
                    fault_proof['pausedSeconds']=time.monotonic()-pause_at
                    self.fault_interval=(pause_wall,time.time()+30)
                    next_offer+=fault_proof['pausedSeconds']
                    fault_proof['producerScheduledPauseSeconds']=fault_proof['pausedSeconds']
                    fault_proof['restoredProgress']=probe(self.base,'probe',self.token,30)
                    blocked=time.monotonic()-pause_at
                    next_offer+=blocked-fault_proof['pausedSeconds']
                    fault_proof['producerScheduledPauseSeconds']=blocked
                    resumed=True
                time.sleep(.02)
            for future in futures:
                record=future.result()
                self.request_times.append(record['requestSeconds'])
                if 'error' in record:self.request_errors.append(record)
                else:self.records.append(record)
        if paused and not resumed:
            for proc in self.agents:os.kill(proc.pid,signal.SIGCONT)
        lab.eventually('all admitted effects and results',self.finished,180)
        rows=self.inspect()
        by_id=collections.defaultdict(list)
        for row in rows:by_id[row['id']].append(row)
        effects=self.effects.read_text().splitlines()
        counts=collections.Counter(effects)
        assert set(counts)==set(by_id) and all(n==1 for n in counts.values()),'Lost or duplicate external effect'
        for ident,attempts in by_id.items():
            expected=2 if attempts[0]['definition_id']=='retry' else 1
            assert len(attempts)==expected and attempts[-1]['state']=='succeeded','Incorrect attempt/result'
            if expected==2:assert attempts[0]['state']=='failed','Retry lost its original failure'
        assert len({r['execution_id'] for r in rows})==len(rows),'Duplicated execution identity'
        assert all(0<r['created_at']<=r['accepted_at']<=r['started_at']<=r['finished_at'] for r in rows),'Incomplete or invalid durable stage receipts'
        assert set(('fast','output','long','retry','http','parent','child','probe'))<=set(r['definition_id'] for r in rows),'Incomplete mixed workload'
        all_stages={name:[] for name in ('ready_to_planned','planned_to_accepted','accepted_to_started','started_to_finished','ready_to_started')}
        stages={name:[] for name in ('ready_to_planned','planned_to_accepted','accepted_to_started','started_to_finished','ready_to_started')}
        for row in rows:
            planned,accepted,started,finished=[row[k] for k in ('created_at','accepted_at','started_at','finished_at')]
            affected=self.fault_interval and planned/1000<=self.fault_interval[1] and finished/1000>=self.fault_interval[0]-30
            def stage(name,value):
                all_stages[name].append(value)
                if not affected:stages[name].append(value)
            for name,a,b in [('planned_to_accepted',planned,accepted),('accepted_to_started',accepted,started),('started_to_finished',started,finished)]:
                if a>0 and b>=a:stage(name,(b-a)/1000)
            if row['attempt']==1 and row['eligibility']:
                from datetime import datetime
                ready=int(datetime.fromisoformat(row['eligibility'].removeprefix('Business gates passed at ').replace('Z','+00:00')).timestamp()*1000)
                if planned>=ready:stage('ready_to_planned',(planned-ready)/1000)
                if started>=ready:stage('ready_to_started',(started-ready)/1000)
        latencies={k:{'observations':len(v),'p95':quantile(v,.95),'p99':quantile(v,.99),'max':max(v) if v else None} for k,v in stages.items()}
        all_latencies={k:{'observations':len(v),'p95':quantile(v,.95),'p99':quantile(v,.99),'max':max(v) if v else None} for k,v in all_stages.items()}
        normal=[s for s in self.samples if not self.fault_interval or not self.fault_interval[0]-30<=start_wall+s['seconds']<=self.fault_interval[1]]
        lag=[s['metrics'].get('regente_audit_oldest_pending_seconds',0) for s in normal]
        oldest=[s['metrics'].get('regente_execution_oldest_outbox_seconds',0) for s in normal]
        maxrss=max(s['serverRSS'] for s in self.samples)
        minimum_density=min(s['metrics'].get('regente_instances{status="OK"}',0) for s in self.samples)
        qualified=(minimum_density>=self.density and not self.request_errors and latencies['planned_to_accepted']['p99'] is not None and latencies['planned_to_accepted']['p99']<=5 and latencies['ready_to_started']['p99'] is not None and latencies['ready_to_started']['p99']<=10 and max(lag,default=0)<=30 and max(oldest,default=0)<=5 and maxrss<=1<<30)
        # Completa a prova fora do orçamento de performance, sem falsificar aceite.
        lab.eventually('audit drained',lambda:self.req('/api/audit/security/status')['pending']==0,300)
        checkpoint=json.loads(self.journal.read_text().splitlines()[-1])
        cp={k:checkpoint[k] for k in ('stream','seq','hash')}
        checkpoint_path=lab.EVIDENCE/(self.name+'-checkpoint.json');checkpoint_path.write_text(json.dumps(cp))
        lab.command([str(lab.RUN/'server'),'-db-driver','postgres','-db',self.dsn,'-audit-key',str(self.key),'-audit-verify','-audit-checkpoint',str(checkpoint_path)],env=self.env,name=self.name+'-audit-verify')
        (lab.EVIDENCE/(self.name+'-attempts.json')).write_text(json.dumps(rows,indent=2))
        (lab.EVIDENCE/(self.name+'-samples.json')).write_text(json.dumps(self.samples,indent=2))
        return {'name':self.name,'retainedRows':self.density,'minimumObservedTerminalRows':minimum_density,'targetOrdersPerDay':round(rate*86400),'measurementSeconds':seconds,'peakMultiplier':4,'peakSecondsPer300':60,'offeredRequests':len(futures),'admittedInstances':len(by_id),'attempts':len(rows),'errors':self.request_errors,'apiRequestSeconds':{'p95':quantile(self.request_times,.95),'p99':quantile(self.request_times,.99)},'latencies':latencies,'allAttemptLatenciesIncludingFault':all_latencies,'faultWallInterval':self.fault_interval,'databaseBytes':int(lab.command(lab.COMPOSE+['exec','-T','postgres','psql','-U','regente','-d',self.dbname,'-Atc','SELECT pg_database_size(current_database())'])),'serverCPUSeconds':cpu_seconds(self.server),'agentCPUSeconds':[cpu_seconds(p) for p in self.agents],'serverPeakRSSBytes':maxrss,'agentPeakRSSBytes':[max(s['agentRSS'][i] for s in self.samples) for i in range(2)],'maxAuditLagSeconds':max(lag,default=0),'maxOutboxAgeSeconds':max(oldest,default=0),'correctness':{'conditions':True,'attemptIdentity':True,'retry':True,'effectsExactlyOnceInThisLab':True,'ledgerCheckpoint':True},'externalCanary':canary,'fault':fault_proof,'qualified':qualified,'allSamplesIncludeFault':True,'normalPerformanceExclusionSeconds':60+fault_proof['pausedSeconds'] if fault_proof else 0,'averageOfferedPerSecond':len(futures)/(seconds-(fault_proof['producerScheduledPauseSeconds'] if fault_proof else 0))}

    def close(self):
        for proc in self.agents:lab.stop_process(proc)
        lab.stop_process(self.server);lab.stop_process(self.collector)
        self.external.shutdown();self.external.server_close()

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--tier-seconds',type=int,default=300);p.add_argument('--soak-seconds',type=int,default=1800)
    a=p.parse_args()
    if a.tier_seconds<300 or a.soak_seconds<1800:raise SystemExit('Qualification requires tiers >=300s and developer soak >=1800s')
    lab.EVIDENCE.mkdir(parents=True)
    report={'datasetPreparation':'Schema32 and actual empty-business backfills initialized before seeding; frozen label/job_type/environment populated; terminal fixtures without execution claims','status':'running','profile':'synthetic-i16-postgres-loopback-v1','tiers':[],'budgets':{'plannedToAcceptedP99Seconds':5,'readyToStartedP99Seconds':10,'oldestOutboxMaxSeconds':5,'auditLagMaxSeconds':30,'serverRSSMaxBytes':1<<30,'normalRequestErrorBudget':0},'limits':['One Linux host; PostgreSQL; production loopback; two agents/5 slots','Short rate windows with retained terminal fixtures; not a full day of executions','No HA/NATS/mTLS throughput or monthly SLO claim; independent pilot I17 pending']}
    started=time.monotonic();active=None;compose=False
    try:
        if platform.system()!='Linux' or platform.machine() not in ('x86_64','amd64'):raise RuntimeError('Mandatory Linux/amd64 profile')
        report['sha']=lab.command(['git','rev-parse','HEAD']);report['dirty']=bool(lab.command(['git','status','--porcelain']))
        report['platform']=platform.platform();report['cpu']=json.loads(lab.command(['lscpu','--json']))
        report['meminfo']=Path('/proc/meminfo').read_text();report['go']=lab.command(['go','version'])
        tls=lab.RUN/'tls';tls.mkdir()
        lab.command(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-days','1','-subj','/CN=capacity-lab','-addext','subjectAltName=IP:127.0.0.1','-keyout',str(tls/'lab.key'),'-out',str(tls/'lab.crt')])
        os.environ['REGENTE_LAB_TLS_DIR']=str(tls)
        env={k:v for k,v in os.environ.items() if not k.startswith(('REGENTE_','OTEL_')) and k not in ('GITHUB_TOKEN','GH_TOKEN')}
        env['SSL_CERT_FILE']=str(tls/'lab.crt')
        lab.command(lab.COMPOSE+['up','-d','postgres'],timeout=300,name='capacity-postgres');compose=True
        dsn='postgres://regente:synthetic-lab-only@127.0.0.1:'+lab.port('postgres',5432)+'/regente_lab?sslmode=disable'
        for source,name,var in [('./server','server','main.version'),('./agent','agent','main.agentVersion'),('./server/cmd/capacity-seed','seed',None)]:
            args=['go','build','-o',str(lab.RUN/name)]
            if var:args+=['-ldflags','-X '+var+'=capacity-'+report['sha'][:12]]
            lab.command(args+[source],timeout=300,name=name+'-build')
        report['postgresSettings']=lab.command(lab.COMPOSE+['exec','-T','postgres','psql','-U','regente','-d','regente_lab','-Atc',"SELECT name||'='||setting FROM pg_settings WHERE name IN ('fsync','synchronous_commit','shared_buffers','max_connections','full_page_writes')"])
        supported=None
        for density in (10000,100000,1000000):
            active=Profile('tier'+str(density),density,env,dsn,tls)
            outcome=active.exercise(density/86400,a.tier_seconds);report['tiers'].append(outcome)
            active.close();active=None
            print('I16 tier '+str(density)+': qualified='+str(outcome['qualified']),flush=True)
            if not outcome['qualified']:
                report['stopReason']='Previous tier exceeded its declared budget; higher tiers not run'
                break
            supported=density
        if supported is None:raise RuntimeError('No supported initial envelope; preserve report and fix measured bottleneck')
        active=Profile('soak'+str(supported),supported,env,dsn,tls)
        report['soak']=active.exercise(supported/86400,a.soak_seconds,fault=True)
        active.close();active=None
        # Falha prevista é visível; nenhuma perda/duplicação ou evidência ausente é permitida.
        if not report['soak']['fault'] or not report['soak']['fault']['stalledProgressDetected']:raise RuntimeError('Missing independent stalled-progress proof')
        report['supportedEnvelope']=supported if report['soak']['qualified'] else None
        report['images']=json.loads(lab.command(lab.COMPOSE+['images','--format','json']))
        if report['supportedEnvelope'] is None:raise RuntimeError('Soak exceeded the supported envelope budget')
        report['status']='passed'
    except Exception as error:
        report['status']='failed';report['error']=str(error);print(str(error),file=sys.stderr,flush=True)
    finally:
        if active:
            try:active.close()
            except Exception as error:report['cleanupError']=str(error)
        for proc,_ in list(lab.PROCESSES):
            try:lab.stop_process(proc)
            except Exception as error:report['cleanupError']=str(error)
        if compose:
            try:lab.command(lab.COMPOSE+['down','-v','--remove-orphans'],name='capacity-cleanup')
            except Exception as error:report['status']='failed';report['cleanupError']=str(error)
        report['seconds']=time.monotonic()-started
        (lab.EVIDENCE/'capacity.json').write_text(json.dumps(report,indent=2)+'\n')
        print('Evidence: '+str(lab.EVIDENCE),flush=True)
    return 0 if report['status']=='passed' else 1
if __name__=='__main__':sys.exit(main())
