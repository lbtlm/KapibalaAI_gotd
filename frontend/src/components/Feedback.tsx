import {Alert,Skeleton} from 'antd'
import {ApiError} from '../api/client'
import styles from '../layout/MainLayout.module.scss'
export function ErrorNotice({error}:{error:unknown}){if(!error)return null;const e=error as ApiError;return <Alert type="error" showIcon title={e.status===403?'没有操作权限':e.message??'加载失败'} description={e.code?e.code+(e.details?.stepIndex?' · 第 '+e.details.stepIndex+' 步 · 变量 '+e.details.key:''):undefined}/>}
export function LoadState({loading,empty,description='数据同步后会显示在这里。'}:{loading?:boolean;empty?:boolean;description?:string}){return loading?<div className={styles.loading} role="status" aria-label="加载中"><Skeleton active={false} title={false} paragraph={{rows:3}}/></div>:empty?<div className={styles.empty}><strong>暂无数据</strong><p>{description}</p></div>:null}
