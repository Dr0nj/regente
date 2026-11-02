import importlib.util
from pathlib import Path
import unittest

spec=importlib.util.spec_from_file_location("compat",Path(__file__).with_name("check-compatibility.py"))
compat=importlib.util.module_from_spec(spec)
spec.loader.exec_module(compat)

class CompatibilityTests(unittest.TestCase):
    def test_runtime_matrix(self):
        server={"component":"server","build":"v-test","schemaMin":32,"schemaMax":32,"protocol":2,"journal":1}
        agent={"component":"agent","build":"v-test","protocol":2,"journal":1}
        self.assertEqual(compat.check(server,agent,32),[])
        for schema in (0,22,23,24,25,26,27,28,29,30,31,33):
            with self.subTest(schema=schema): self.assertTrue(compat.check(server,agent,schema))
        for field,value in (("build","other"),("build",""),("protocol",1),("protocol",3),("journal",0),("journal",2),("component","server")):
            with self.subTest(field=field,value=value): self.assertTrue(compat.check(server,dict(agent,**{field:value}),32))
        self.assertTrue(compat.check({},agent,32))

if __name__=="__main__":unittest.main()
