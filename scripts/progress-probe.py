#!/usr/bin/env python3
"""Order one approved canary and require a durable execution result (not health alone)."""
import argparse,json,os,time,urllib.request,urllib.error
def request(base,path,token,method='GET'):
    req=urllib.request.Request(base+path,method=method,headers={'Authorization':'Bearer '+token})
    with urllib.request.urlopen(req,timeout=5) as r:return json.load(r)
def probe(base,definition,token,deadline=5):
    start=time.monotonic()
    out=request(base,'/api/definitions/'+definition+'/force',token,'POST')
    instance=out['instanceId']
    while time.monotonic()-start<deadline:
        row=request(base,'/api/instances/'+instance,token)
        if row['status']=='OK':
            attempts=request(base,'/api/instances/'+instance+'/executions',token)['attempts']
            if len(attempts)==1 and attempts[0]['state']=='succeeded' and attempts[0]['acceptedAt']>0 and attempts[0]['startedAt']>0 and time.monotonic()-start<=deadline:
                return {'passed':True,'instanceId':instance,'executionId':attempts[0]['executionId'],'seconds':time.monotonic()-start}
        if row['status'] in ('NOTOK','UNCERTAIN','CANCELLED'):raise RuntimeError('Canary did not succeed: '+row['status'])
        time.sleep(.2)
    raise RuntimeError('No durable canary progress by deadline; instance='+instance)
if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--base',required=True);p.add_argument('--definition',required=True)
    p.add_argument('--token-file',required=True);p.add_argument('--deadline',type=float,default=5)
    a=p.parse_args()
    print(json.dumps(probe(a.base,a.definition,open(a.token_file).read().strip(),a.deadline)))
