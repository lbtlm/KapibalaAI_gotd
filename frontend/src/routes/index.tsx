import {useEffect,useState} from 'react'
import {createBrowserRouter,Navigate,Outlet} from 'react-router-dom'
import {Spin} from 'antd'
import {api} from '../api/client'
import {useSession} from '../hooks'
import MainLayout from '../layout/MainLayout'
import Login from '../pages/Login'
import Accounts from '../pages/Accounts'
import Groups from '../pages/Groups'
import GroupDetail from '../pages/GroupDetail'
import AgentRun from '../pages/AgentRun'
import Sequences from '../pages/Sequences'
function AuthGuard(){
 const session=useSession(),[ready,setReady]=useState(!!session)
 useEffect(()=>{if(!session&&!ready)api.refresh().catch(()=>{}).finally(()=>setReady(true))},[session,ready])
 return !ready?<Spin aria-label="恢复会话"/>:session?<Outlet/>:<Navigate to="/login" replace/>
}
export const router=createBrowserRouter([
 {path:'/login',element:<Login/>},
 {element:<AuthGuard/>,children:[{element:<MainLayout/>,children:[{path:'/',element:<Navigate to="/accounts" replace/>},{path:'/accounts',element:<Accounts/>},{path:'/groups',element:<Groups/>},{path:'/groups/:id',element:<GroupDetail/>},{path:'/agent-runs/:id',element:<AgentRun/>},{path:'/sequences',element:<Sequences/>}]}]},
 {path:'*',element:<Navigate to="/" replace/>}
],{future:{v7_relativeSplatPath:true}})

