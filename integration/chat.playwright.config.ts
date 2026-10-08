import { defineConfig } from '@playwright/test'
export default defineConfig({ testDir: '.', testMatch: 'chat.spec.ts', timeout: 60000, workers: 1, reporter: 'list', use: { baseURL: process.env.BASE_URL || 'http://127.0.0.1:3002', trace: 'retain-on-failure' } })
