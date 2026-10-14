import {useState} from 'react'
import {Button,Form,Input,Alert} from 'antd'
import {useNavigate} from 'react-router-dom'
import {api} from '../api/client'
import {ErrorNotice} from '../components/Feedback'
import styles from '../layout/MainLayout.module.scss'
export default function Login(){
 const navigate=useNavigate()
 const [busy,setBusy]=useState(false),[error,setError]=useState<unknown>(),[done,setDone]=useState(false)
 async function submit(value:{username:string;password:string}){setBusy(true);setError(null);try{await api.login(value.username,value.password);setDone(true);navigate('/accounts')}catch(e){setError(e)}finally{setBusy(false)}}
 return <main className={styles.login}><div className={styles.loginMark}>Kapibala<small>消息操作台</small></div><h1>登录工作台</h1><p className={styles.loginIntro}>使用管理员或只读账号进入。</p><div className={styles.loginForm}><ErrorNotice error={error}/>{done&&<Alert type="success" title="登录成功"/>}<Form layout="vertical" onFinish={submit}><Form.Item label="用户名" name="username" rules={[{required:true,message:'请输入用户名'}]}><Input autoComplete="username"/></Form.Item><Form.Item label="密码" name="password" rules={[{required:true,message:'请输入密码'}]}><Input.Password autoComplete="current-password"/></Form.Item><Button aria-label="登录" type="primary" htmlType="submit" loading={busy} block>登录</Button></Form><div className={styles.loginNote}><p>演示账号</p><p><code>admin / admin</code> 管理员</p><p><code>viewer / viewer</code> 只读观察员</p></div></div></main>
}
