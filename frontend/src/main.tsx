import React from 'react'
import ReactDOM from 'react-dom/client'
import {RouterProvider} from 'react-router-dom'
import {App,ConfigProvider} from 'antd'
import zhCN from 'antd/locale/zh_CN'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {router} from './routes'
const queryClient=new QueryClient({defaultOptions:{queries:{retry:false,staleTime:0}}})
ReactDOM.createRoot(document.getElementById('root')!).render(<React.StrictMode><QueryClientProvider client={queryClient}><ConfigProvider locale={zhCN} theme={{token:{colorPrimary:'#315c7b',colorInfo:'#315c7b',colorSuccess:'#2d6150',colorWarning:'#805718',colorError:'#963c35',colorTextPlaceholder:'#626f7c',colorBgLayout:'#f4f5f7',colorText:'#26313c',colorTextSecondary:'#4b5865',colorTextTertiary:'#626f7c',colorBorder:'#dfe4e8',borderRadius:4,fontFamily:'"Segoe UI", "Microsoft YaHei", sans-serif',controlHeight:40},components:{Input:{colorBgContainer:'#f6f8fa'},Select:{colorBgContainer:'#f6f8fa'},Table:{cellPaddingBlock:8,cellPaddingInline:12,headerBg:'#f6f8fa',headerColor:'#626f7c'},Button:{defaultShadow:'none',primaryShadow:'none',dangerShadow:'none'},Alert:{colorError:'#963c35',colorSuccess:'#2d6150'}}}}><App><RouterProvider router={router} future={{v7_startTransition:true}}/></App></ConfigProvider></QueryClientProvider></React.StrictMode>)
