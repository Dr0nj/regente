import copy
import json
import tempfile
from pathlib import Path
import unittest
import capacity


class PerformanceContracts(unittest.TestCase):
    def test_graph_is_executable_and_dependencies_have_prior_producers(self):
        templates={kind:{'id':kind,'params':{'command':'effect'},'conditionsIn':['legacy'],'conditionsOutAdd':['legacy']}
                   for kind in ('fast','output','long','retry','http','child','parent')}
        original=copy.deepcopy(templates)
        graph=capacity.performance_workload(templates)
        self.assertEqual(len(graph),28800)
        self.assertEqual(len({name for name,_,_ in graph}),28800)
        self.assertEqual({kind for _,kind,_ in graph},set(templates))
        produced=set()
        for name,_,definition in graph:
            self.assertEqual(name,definition['id'])
            self.assertEqual(definition['schedule']['keepActive'],2)
            self.assertTrue(set(definition['conditionsIn'])<=produced)
            self.assertIn('command',definition['params'])
            produced.update(definition['conditionsOutAdd'])
        graph[0][2]['params']['command']='changed'
        self.assertEqual(templates,original)
        self.assertEqual(graph[1][2]['params']['command'],'effect')

    def test_checkpoint_reads_tail_and_rejects_partial_write(self):
        with tempfile.TemporaryDirectory() as folder:
            path=Path(folder)/'journal.jsonl'
            checkpoint={'stream':'test','seq':123,'hash':'signed-hash'}
            path.write_bytes(b'old record\n'*200000+json.dumps(checkpoint).encode()+b'\n')
            self.assertEqual(capacity.last_checkpoint(path),checkpoint)
            with path.open('ab') as journal:journal.write(b'{"incomplete":')
            with self.assertRaises(RuntimeError):capacity.last_checkpoint(path)

    def test_daily_gate_rejects_row_count_and_repeated_definitions(self):
        valid={'qualified':True,'measurementSeconds':86400,'admittedInstances':30000,
               'distinctGraphDefinitionsExecuted':28800,
               'businessCalendar':{'orderDate':'2026-11-09'},
               'businessCalendarAtEnd':{'orderDate':'2026-11-10'}}
        capacity.validate_performance_day(valid)
        for changes in ({'qualified':False},{'measurementSeconds':86399},
                        {'admittedInstances':28799},{'distinctGraphDefinitionsExecuted':8},
                        {'businessCalendarAtEnd':{'orderDate':'2026-11-09'}}):
            with self.subTest(changes=changes), self.assertRaises(RuntimeError):
                capacity.validate_performance_day(dict(valid,**changes))


if __name__=='__main__':unittest.main()
