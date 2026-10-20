import { defineConfig } from '@playwright/test'
export default defineConfig({testDir:'.',testMatch:'compact.spec.ts',outputDir:'../.lidza/compact-media-results',timeout:90000,workers:1,reporter:'list',use:{baseURL:process.env.BASE_URL || 'http://127.0.0.1:18080',trace:'retain-on-failure',launchOptions:{args:['--use-fake-ui-for-media-stream','--use-fake-device-for-media-stream']}}})
