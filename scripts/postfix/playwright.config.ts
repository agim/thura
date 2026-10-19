import { defineConfig } from '@playwright/test'
export default defineConfig({
 testDir: '.', testMatch: 'wizard.spec.ts', workers: 1, retries: 0,
 use: { baseURL: 'http://127.0.0.1:8082', browserName: 'chromium' },
 outputDir: '../../.lidza/postfix-wizard-results',
})
