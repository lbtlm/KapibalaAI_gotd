import React from 'react'
import ReactDOM from 'react-dom/client'
import {RouterProvider} from 'react-router-dom'
import {App,ConfigProvider} from 'antd'
import zhCN from 'antd/locale/zh_CN'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {router} from './routes'
const queryClient=new QueryClient({defaultOptions:{queries:{retry:false,staleTime:0}}})
ReactDOM.createRoot(document.getElementById('root')!).render(<React.StrictMode><QueryClientProvider client={queryClient}><ConfigProvider locale={zhCN} theme={{token:{colorPrimary:'#267f77',colorBgLayout:'#f7f8fa',colorText:'#202b3b',borderRadius:6,fontFamily:'"Segoe UI", "Microsoft YaHei", sans-serif',controlHeight:40}}}><App><RouterProvider router={router} future={{v7_startTransition:true}}/></App></ConfigProvider></QueryClientProvider></React.StrictMode>)

