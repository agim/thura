import unittest
from client import classify, impossible
from hybrid import route, require_limits
from codex_pilot import blind_items, validate_answers, command


class HybridContractTests(unittest.TestCase):
    def test_local_findings_cannot_be_overridden(self):
        for local in ('spam','phishing'):
            for review in ({'category':'legitimate'},{'error':'timeout'},{}):
                self.assertEqual(route(local,review),local)

    def test_all_remaining_messages_require_complete_review(self):
        for review in ({},{'category':'unknown'},{'category':'safe'},{'category':'legitimate','error':'timeout'}):
            with self.assertRaises(ValueError): route('legitimate',review)
        for category in ('legitimate','spam','phishing'):
            self.assertEqual(route('legitimate',{'category':category}),category)
        with self.assertRaises(ValueError): route('error',{'category':'legitimate'})

    def test_fixed_local_thresholds(self):
        for score,expected in ((-10,'legitimate'),(0,'legitimate'),(5.99,'legitimate'),(6,'spam'),(15,'spam')):
            self.assertEqual(classify({'score':score})[0],expected)
        self.assertEqual(classify({'score':-10,'symbols':{'PHISHING':{}}})[0],'phishing')
        for reply in ({'score':0,'is_skipped':True},{'score':0,'error':'DNS'},{'score':float('nan')}):
            with self.assertRaises(ValueError): classify(reply)

    def test_rejection_uses_planned_cohort_not_partial_percentages(self):
        totals={'legitimate':150,'spam':75,'phishing':75}
        rows=[{'label':'legitimate','prediction':'spam','source':'real'}]*3
        self.assertEqual(impossible(rows,totals),[])
        self.assertTrue(impossible(rows+[rows[0]],totals))
        rows=[{'label':'phishing','prediction':'spam','source':'real'}]*4
        self.assertIn('phishing detection cannot reach 95%',impossible(rows,totals))
        self.assertTrue(impossible([{'label':'spam','prediction':'error','source':'real','error':'timeout'}],totals))

    def test_resource_cap_is_enforced(self):
        group={'memory.max':str(2*1024**3),'memory.swap.max':'0','cpu.max':'200000 100000'}
        require_limits(group)
        for key,value in (('memory.max','max'),('memory.swap.max','2147483648'),('cpu.max','max 100000')):
            with self.assertRaises(ValueError): require_limits(dict(group,**{key:value}))

    def test_codex_gets_no_labels_or_local_findings(self):
        items,mapping=blind_items([
            {'id':'first','label':'phishing','source':'gold-source','local_prediction':'legitimate','score':0},
            {'id':'second','label':'legitimate','local_prediction':'spam'}],[],{'first':'Opaque message evidence'})
        self.assertEqual(items,[{'id':'1','evidence':{'text':'Opaque message evidence'}}])
        self.assertEqual(mapping,[{'id':'1','original_id':'first','kind':'remaining'}])

    def test_codex_answers_must_be_complete_and_unique(self):
        mapping=[{'id':'1'},{'id':'2'}]
        valid=[{'id':i,'category':'legitimate','explanation':'Assessed supplied evidence'} for i in ['1','2']]
        self.assertEqual(len(validate_answers({'verdicts':valid},mapping)),2)
        for verdicts in (valid[:1],[valid[0],valid[0]],[valid[0],dict(valid[1],category='safe')]):
            with self.assertRaises(ValueError): validate_answers({'verdicts':verdicts},mapping)
        args=command('/tmp/private-eval')
        self.assertIn('--ignore-user-config',args)
        self.assertIn('--ephemeral',args)
        self.assertIn('read-only',args)
        self.assertIn('features.shell_tool=false',args)
        self.assertIn('features.apps=false',args)
        self.assertIn('features.multi_agent=false',args)
        self.assertIn('web_search="disabled"',args)


if __name__=='__main__': unittest.main()
