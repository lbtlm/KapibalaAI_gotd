import {defineConfig,devices} from '@playwright/test'
const stack=process.env.E2E_STACK_COMMAND
const baseURL=process.env.E2E_BASE_URL??'http://127.0.0.1:5173'
const backend=process.env.E2E_API_URL??'http://127.0.0.1:8080'
export default defineConfig({
 testDir:'./e2e',timeout:90000,expect:{timeout:15000},fullyParallel:false,workers:1,retries:process.env.CI?1:0,
 reporter:[['list'],['html',{open:'never'}]],use:{baseURL,trace:'retain-on-failure',screenshot:'only-on-failure',...devices['Desktop Chrome'],channel:process.env.E2E_BROWSER_CHANNEL||undefined},
 webServer:[...(stack?[{command:stack,cwd:'..',url:backend+'/api/health',timeout:180000,reuseExistingServer:!process.env.CI}]:[]),{command:'pnpm dev --port '+(new URL(baseURL).port||'5173'),url:baseURL,reuseExistingServer:!process.env.CI,timeout:30000}]
})
