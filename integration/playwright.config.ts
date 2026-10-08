import { defineConfig } from '@playwright/test'
export default defineConfig({ testDir: '.', outputDir: '../.lidza/test-results-office', testMatch: 'office.spec.ts', timeout: 120000, workers: 1, reporter: 'list', use: { baseURL: process.env.BASE_URL || 'http://127.0.0.1:3002', trace: 'retain-on-failure' } })
