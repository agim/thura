"""Check resource budgets against real Compose merging, without starting services."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]
CATALOG = json.loads(Path(__file__).with_name('catalog.json').read_text())['profiles']


class ProfileTests(unittest.TestCase):
    def test_all_profiles_reserve_host_memory_and_fit_their_cpu_budget(self):
        previous = None
        for name in ['minimum', 'standard', 'performance']:
            profile = CATALOG[name]
            services = profile['services']
            self.assertEqual(set(services), {'thura','postgres','postfix','collabora'})
            self.assertEqual(sum(row['memory_mib'] for row in services.values()) + profile['host_reserve_mib'],profile['host_memory_mib'])
            self.assertEqual(sum(row['cpus'] for row in services.values()),profile['host_cpu'])
            if previous:
                for key in services:
                    self.assertGreater(services[key]['memory_mib'],previous[key]['memory_mib'])
                    self.assertGreater(services[key]['cpus'],previous[key]['cpus'])
            previous = services
        self.assertEqual(CATALOG['minimum']['host_memory_mib'],4096)
        self.assertEqual(CATALOG['standard']['validation'],'sizing_proposal')
        self.assertEqual(CATALOG['performance']['validation'],'sizing_proposal')

    def test_compose_resolves_to_exactly_four_services_with_the_selected_limits(self):
        executable = shutil.which('docker-compose')
        command = [executable] if executable else ['docker','compose']
        environment = dict(os.environ)
        for key in ['POSTGRES_PASSWORD','AUTH_SECRET','LIDZA_MASTER_KEY','THURA_SETUP_TOKEN','THURA_POSTFIX_TOKEN','THURA_RSPAMD_PASSWORD','LIVEKIT_API_KEY','LIVEKIT_API_SECRET','COLLABORA_TOKEN_SECRET']:
            environment[key] = 'synthetic-profile-validation-value-0123456789'
        environment['COLLABORA_IMAGE']='collabora/code@sha256:'+'0'*64
        for name,profile in CATALOG.items():
            args = command + ['-f',str(ROOT/'deploy/compose.yaml')]
            if name!='minimum':args += ['-f',str(Path(__file__).with_name(name+'.compose.json'))]
            result = subprocess.run(args+['config','--format','json'],env=environment,text=True,capture_output=True,check=True)
            services = json.loads(result.stdout)['services']
            self.assertEqual(set(services),set(profile['services']))
            for key,limits in profile['services'].items():
                self.assertEqual(int(services[key]['mem_limit']),limits['memory_mib']*1024*1024)
                self.assertEqual(int(services[key]['memswap_limit']),int(services[key]['mem_limit']))
                self.assertEqual(float(services[key]['cpus']),limits['cpus'])
            self.assertEqual(services['postfix']['network_mode'],'service:thura')
            self.assertEqual(services['thura']['environment']['CACHE_URL'],'redis://127.0.0.1:6379/1')
            self.assertEqual(services['thura']['environment']['REALTIME_BUS_URL'],'redis://127.0.0.1:6379/2')


if __name__ == '__main__':unittest.main()
