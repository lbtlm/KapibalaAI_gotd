import {useMemo,useState} from 'react'
import {useMutation,useQueryClient} from '@tanstack/react-query'
import {Alert,Button,Form,Modal,Select,Table,Tag} from 'antd'
import {api} from '../api/client'
import type {AccountGroup,AccountImportResult} from '../types'
import {AccountErrorNotice as ErrorNotice,accountErrorText} from './AccountFeedback'
import styles from '../layout/MainLayout.module.scss'
export default function ImportAccounts({groups,onClose}:{groups:AccountGroup[];onClose:()=>void}){
 const cache=useQueryClient(),[files,setFiles]=useState<File[]>([]),[groupId,setGroupId]=useState<string>(),[results,setResults]=useState<AccountImportResult[]>([])
 const pairs=useMemo(()=>{
  const byName=new Map<string,{basename:string;files:File[]}>()
  for(const file of files){const basename=file.name.replace(/\.(session|json)$/i,'');const item=byName.get(basename)??{basename,files:[]};item.files.push(file);byName.set(basename,item)}
  return [...byName.values()].map(pair=>{const sessions=pair.files.filter(f=>/\.session$/i.test(f.name)),metadata=pair.files.filter(f=>/\.json$/i.test(f.name));const error=pair.files.some(f=>! /\.(session|json)$/i.test(f.name))?'仅支持 .session 和 .json 文件':sessions.length>1||metadata.length>1?'同名文件重复，无法唯一配对':!sessions.length?'缺少同名 .session 文件':!metadata.length?'缺少同名 .json 文件':null;return {...pair,error}})
 },[files])
 const upload=useMutation({mutationFn:()=>{const body=new FormData();files.forEach(f=>body.append('files',f));if(groupId)body.append('groupId',groupId);return api.request<{results:AccountImportResult[]}>('/api/accounts/import','POST',body)},onSuccess:async r=>{setResults(r.results);await Promise.all([cache.invalidateQueries({queryKey:['accounts']}),cache.invalidateQueries({queryKey:['account-groups']})])}})
 const ready=pairs.length>0&&pairs.every(p=>!p.error)
 return <Modal title="上传账号" open width={840} footer={null} onCancel={()=>{if(!upload.isPending)onClose()}} closable={!upload.isPending} mask={{closable:!upload.isPending}} keyboard={!upload.isPending}>
  <p>同时选择同名的 Telethon SQLite .session 与 .json 资料文件，可一次导入多组。导入后为空闲状态，连接需主动操作。</p>
  <ErrorNotice error={upload.error}/>
  <Form layout="vertical"><Form.Item label="导入到分组"><Select aria-label="导入到分组" placeholder="未分组" allowClear value={groupId} disabled={upload.isPending} onChange={setGroupId} options={groups.map(g=>({value:g.id,label:g.name}))}/></Form.Item><Form.Item label="账号文件" htmlFor="account-files" extra="重新选择会替换本批次。文件仅用于此次导入，不在页面展示凭据内容。"><input id="account-files" type="file" multiple accept=".session,.json" disabled={upload.isPending} onChange={e=>{setFiles(Array.from(e.target.files??[]));setResults([]);upload.reset()}}/></Form.Item></Form>
  {pairs.length>0&&<><p>{pairs.filter(p=>!p.error).length} 组配对完整</p><Table size="small" rowKey="basename" pagination={false} scroll={{y:240}} dataSource={pairs} columns={[{title:'文件组',dataIndex:'basename'},{title:'文件',render:(_,p)=>p.files.map(f=>f.name).join('、')},{title:'预览',render:(_,p)=>p.error?<TypographyError text={p.error}/>:<Tag>可导入</Tag>},{title:'操作',render:(_,p)=><Button type="text" disabled={upload.isPending} onClick={()=>{setFiles(files.filter(f=>!p.files.includes(f)));setResults([]);upload.reset()}}>移除</Button>}]}/></>}
  {pairs.some(p=>p.error)&&<Alert type="warning" title="请补齐或移除有问题的文件组后再导入。"/>}
  {results.length>0&&<><Alert type="info" title={'本批次完成：成功 '+results.filter(r=>r.success).length+'，失败 '+results.filter(r=>!r.success).length+'。重新选择文件可开始下一批。'}/><Table size="small" rowKey="basename" pagination={false} dataSource={results} columns={[{title:'文件组',dataIndex:'basename'},{title:'账号',dataIndex:'accountId',render:v=>v??'-'},{title:'导入结果',render:(_,r)=>r.success?<Tag color="success">导入成功</Tag>:<TypographyError text={accountErrorText(r.error?.code??'',r.error?.message)+(r.error?.code?' · '+r.error.code:'')}/>}]} scroll={{y:240}}/></>}
  <div className={styles.importFooter}><Button onClick={onClose} disabled={upload.isPending} aria-label="关闭">关闭</Button><Button type="primary" loading={upload.isPending} disabled={!ready||results.length>0} onClick={()=>upload.mutate()}>开始导入</Button></div>
 </Modal>
}
function TypographyError({text}:{text:string}){return <span className={styles.checkFailed}>{text}</span>}
