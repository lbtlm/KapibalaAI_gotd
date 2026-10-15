import {useState} from 'react'
import {useMutation,useQuery} from '@tanstack/react-query'
import {Button,Form,Input,Modal,message} from 'antd'
import {api} from '../api/client'
import styles from '../layout/MainLayout.module.scss'

export function useAgentStatus(){return useQuery({queryKey:['agent-status'],queryFn:()=>api.request<{mode:string;model:string;configured:boolean}>('/api/agent/status'),staleTime:30000})}
export default function AgentPreview({context=''}:{context?:string}){
 const [open,setOpen]=useState(false),[notice,holder]=message.useMessage(),status=useAgentStatus()
 const preview=useMutation({mutationFn:(input:{message:string;context:string})=>api.request<{reply:string;model:string}>('/api/agent/reply-preview','POST',input),onError:(e:{code?:string})=>notice.error(({AI_NOT_CONFIGURED:'尚未配置真实 AI，请在服务端配置模型和 API Key。',AI_TIMEOUT:'模型响应超时，请稍后重试。',AI_INVALID_REPLY:'模型返回了无效回复，请重新生成。'} as Record<string,string>)[e.code??'']??'AI 生成失败，请检查模型服务配置后重试。',4)})
 return <>{holder}<Button onClick={()=>{preview.reset();setOpen(true)}}>AI 回复预演</Button><Modal title="AI 回复预演" width={680} open={open} onCancel={()=>setOpen(false)} footer={null} destroyOnHidden>
  <p className={styles.meta}>使用真实模型生成模拟回复，仅在此预览，不向群内发送消息。</p><p className={styles.meta}>模型：{status.data?.model??'读取中'} · 群内模式：{status.data?.mode==='openai'?'真实 AI':status.data?.mode==='scripted'?'固定回复':'读取中'}</p>
  <Form layout="vertical" disabled={preview.isPending} initialValues={{context,message:''}} onFinish={v=>preview.mutate(v)} onValuesChange={()=>preview.reset()}><Form.Item name="context" label="群背景 / 回复要求" extra="填写群用途、活动安排或需要回答的背景信息。"><Input.TextArea rows={3} maxLength={4000}/></Form.Item><Form.Item name="message" label="模拟外部成员消息" rules={[{required:true,whitespace:true,message:'填写要测试的成员消息'}]}><Input.TextArea rows={2} maxLength={2000} placeholder="例如：读书会几点开始，需要提前准备什么？"/></Form.Item><Button type="primary" htmlType="submit" loading={preview.isPending}>生成 AI 回复</Button></Form>
  {preview.data&&<section className={styles.aiReply} aria-label="AI 模拟回复"><h2>AI 模拟回复</h2><p>{preview.data.reply}</p><small>由 {preview.data.model} 生成 · 未发送</small></section>}
 </Modal></>
}
