import {defineConfig} from 'vitest/config'
import {loadEnv} from 'vite'
import react from '@vitejs/plugin-react'
export default defineConfig(({mode})=>{
 const backend=process.env.VITE_BACKEND_URL??loadEnv(mode,process.cwd(),'VITE_').VITE_BACKEND_URL??'http://127.0.0.1:8080'
 return {plugins:[react()],server:{port:5173,strictPort:true,proxy:{'/api':backend,'/ws':{target:backend.replace(/^http/,'ws'),ws:true}}},test:{environment:'jsdom',setupFiles:['./src/tests/setup.ts'],include:['src/**/*.test.{ts,tsx}'],globals:true}}
})
