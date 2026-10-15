import {useEffect,useRef,useState} from 'react'
import {Layout,Menu,Button,Alert} from 'antd'
import {UserOutlined,TeamOutlined,OrderedListOutlined,LogoutOutlined,ApiOutlined} from '@ant-design/icons'
import {Outlet,useNavigate,useLocation} from 'react-router-dom'
import {useQueryClient} from '@tanstack/react-query'
import {api} from '../api/client'
import {invalidateAccountState} from '../api/account-cache'
import {RealtimeClient,type ConnectionState,type StreamEvent} from '../api/realtime'
import {useSession} from '../hooks'
import styles from './MainLayout.module.scss'
const {Header,Sider,Content}=Layout
export default function MainLayout(){
 const session=useSession(),navigate=useNavigate(),location=useLocation(),cache=useQueryClient()
 const [connection,setConnection]=useState<ConnectionState>('connecting'),[incident,setIncident]=useState<string|null>(null)
 const stream=useRef<RealtimeClient>()
 useEffect(()=>{
  const event=(event:StreamEvent)=>{
   const p=event.payload
   if(event.type==='inconsistency')setIncident(String(p.message??'网关与数据库状态出现差异'))
   if(event.type==='account_status_changed'||event.type==='account_terminal'){void invalidateAccountState(cache);void cache.invalidateQueries({queryKey:['group']})}
   if(event.type==='message')void cache.invalidateQueries({queryKey:['messages',p.groupId]})
   if(event.type==='agent_run'){void cache.invalidateQueries({queryKey:['agent-runs',p.groupId]});void cache.invalidateQueries({queryKey:['agent-run',p.runId]})}
   if(event.type==='sequence_run')void cache.invalidateQueries({queryKey:['sequence-run',p.runId]})
   void cache.invalidateQueries({queryKey:['groups']})
   if(p.groupId)void cache.invalidateQueries({queryKey:['group',p.groupId]})
  }
  stream.current=new RealtimeClient(()=>api.getSession()?.accessToken,()=>api.refresh(),event,state=>{setConnection(state);if(state==='connected')void cache.invalidateQueries()})
  stream.current.start()
  return()=>stream.current?.stop()
 },[cache])
 async function logout(){try{await api.logout()}finally{cache.clear();navigate('/login')}}
 const active=location.pathname.startsWith('/groups')||location.pathname.startsWith('/agent-runs')?'/groups':location.pathname.startsWith('/sequences')?'/sequences':'/accounts'
 return <Layout className={styles.layoutContainer}><Sider width={176} className={styles.sider}><div className={styles.sidebarInner}><span className={styles.logo}>Kapibala<small>消息台</small></span><Menu className={styles.nav} selectedKeys={[active]} onClick={({key})=>navigate(key)} items={[{key:'/accounts',icon:<UserOutlined/>,label:'服务账号'},{key:'/groups',icon:<TeamOutlined/>,label:'群组工作区'},{key:'/sequences',icon:<OrderedListOutlined/>,label:'定时序列'}]}/><div className={styles.sidebarFooter}><div className={styles.connection+' '+(connection==='connected'?styles.connectionConnected:'')}><ApiOutlined/>{connection==='connected'?'实时已连接':connection==='offline'?'实时已断开':'正在恢复连接'}</div><span>消息和运行自动同步</span></div></div></Sider><Layout className={styles.mainLayout}><Header className={styles.header}><div className={styles.headerTitle}><strong>消息操作台</strong><span>{active==='/accounts'?'服务账号':active==='/groups'?'群组工作区':'定时序列'}</span></div><div className={styles.headerAccount}><span>{session?.role==='admin'?'管理员':'只读观察员'}</span><Button type="text" icon={<LogoutOutlined/>} onClick={logout}>退出登录</Button></div></Header><Content className={styles.content+(active==='/accounts'?' '+styles.contentAccounts:'')}>{incident&&<Alert type="error" showIcon closable onClose={()=>setIncident(null)} title="状态一致性提醒" description={incident}/>}<Outlet/></Content></Layout></Layout>
}
