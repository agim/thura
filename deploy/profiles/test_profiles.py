"""Check resource budgets against real Compose merging, without starting services."""
import json
import os
import re
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

    def test_deploy_manifest_preserves_compact_service_and_persistence_boundaries(self):
        manifest=json.loads((ROOT/'deploy/lidza.stack.json').read_text())
        self.assertEqual(manifest['version'],2)
        self.assertEqual(set(manifest['profile_resources']),set(CATALOG))
        roles={'app':'thura','mail':'postfix','office':'collabora'}
        for name,profile in CATALOG.items():
            budgets=manifest['profile_resources'][name]
            self.assertEqual(set(budgets['services']),set(roles))
            for role,service in roles.items():
                budget=budgets['services'][role]
                self.assertEqual(budget['memory_mib'],profile['services'][service]['memory_mib'])
                self.assertIsInstance(budget['cpus'],str)
                self.assertEqual(float(budget['cpus']),profile['services'][service]['cpus'])
            self.assertEqual(budgets['database']['memory_mib'],profile['services']['postgres']['memory_mib'])
            self.assertIsInstance(budgets['database']['cpus'],str)
            self.assertEqual(float(budgets['database']['cpus']),profile['services']['postgres']['cpus'])
        self.assertEqual(manifest['bootstrap_readiness'],{'path':'/api/v1/health','readyz_body_contains':'Server setup is required','setup_path':'/setup'})
        services={item['role']:item for item in manifest['services']}
        self.assertEqual(set(services),{'app','mail','office'})
        self.assertIn('LIDZA_MASTER_KEY',manifest['required_environment'])
        self.assertNotIn('LIDZA_MASTER_KEY',manifest['generated_secrets'])
        app=services['app']
        self.assertNotIn('volumes',app)
        self.assertEqual(app['environment']['THURA_MANAGED_STATE_IN_STORAGE'],'true')
        self.assertEqual(app['build']['target'],'compact')
        self.assertEqual(app['environment']['CACHE_URL'],'redis://127.0.0.1:6379/1')
        self.assertEqual(app['environment']['REALTIME_BUS_URL'],'redis://127.0.0.1:6379/2')
        self.assertNotIn('DATABASE_URL',app['environment'])
        self.assertEqual({(p['port'],p['protocol']) for p in app['public_ports']},{(25,'tcp'),(7881,'tcp'),(7882,'udp')})
        self.assertTrue(services['mail']['share_app_network'])
        self.assertEqual({v['name'] for v in services['mail']['volumes']},{'postfix-queue','rspamd-state','clamav-signatures','mail-filter-learning'})
        for service in services.values():
            self.assertNotIn('host_network',service)
            self.assertNotIn('privileged',service)
            for volume in service.get('volumes',[]):
                self.assertNotIn('source',volume)
        office_dockerfile=ROOT/services['office']['build']['dockerfile']
        vendor=re.search(r'^FROM (collabora/code@sha256:[a-f0-9]{64})$',office_dockerfile.read_text(),re.M)
        self.assertIsNotNone(vendor)
        self.assertRegex(vendor.group(1),r'^collabora/code@sha256:[a-f0-9]{64}$')
        self.assertEqual(services['office']['health_command'],['/usr/local/bin/thura-office-health'])
        self.assertIn('CMD ["/usr/local/bin/thura-office-health"]',office_dockerfile.read_text())

    def test_first_install_delegates_required_mail_and_ai_setup_to_thura(self):
        manifest=json.loads((ROOT/'deploy/lidza.stack.json').read_text())
        settings={item['name']:item for item in manifest['configuration']}
        self.assertEqual(len(settings),len(manifest['configuration']))
        self.assertEqual({name:item['stage'] for name,item in settings.items()}, {
            'LIDZA_MASTER_KEY':'deploy', 'MAIL_PROVIDER':'setup',
            'MAIL_FROM':'setup', 'LLM_PROVIDER':'setup'})
        self.assertTrue(settings['LIDZA_MASTER_KEY']['secret'])
        for name in ['MAIL_PROVIDER','MAIL_FROM','LLM_PROVIDER']:
            self.assertNotIn(name,manifest['required_environment'])
            self.assertNotIn(name,manifest['generated_secrets'])
            for service in manifest['services']:
                self.assertNotIn(name,service.get('environment',{}))
        self.assertEqual(manifest['bootstrap_readiness']['setup_path'],'/setup')
        for item in settings.values():
            self.assertTrue(item['label'])
            self.assertTrue(item['help'])

    def test_compose_resolves_to_exactly_four_services_with_the_selected_limits(self):
        executable = shutil.which('docker-compose')
        command = [executable] if executable else ['docker','compose']
        environment = dict(os.environ)
        for key in ['POSTGRES_PASSWORD','AUTH_SECRET','LIDZA_MASTER_KEY','THURA_SETUP_TOKEN','THURA_POSTFIX_TOKEN','THURA_RSPAMD_PASSWORD','LIVEKIT_API_KEY','LIVEKIT_API_SECRET','COLLABORA_TOKEN_SECRET','MATRIX_AS_TOKEN','MATRIX_HS_TOKEN']:
            environment[key] = 'synthetic-profile-validation-value-0123456789'
        environment['APP_DOMAIN']='thura.example.test'
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
