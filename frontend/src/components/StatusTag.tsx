import {Tag} from 'antd'
import styles from '../layout/MainLayout.module.scss'
const statuses:Record<string,{text:string;tone:string}>={
 idle:{text:'空闲',tone:'Neutral'},online:{text:'在线',tone:'Positive'},rate_limited:{text:'限流中',tone:'Warning'},disconnected:{text:'已离线',tone:'Neutral'},suspended:{text:'已停用',tone:'Danger'},session_expired:{text:'会话失效',tone:'Danger'},
 active:{text:'正常',tone:'Positive'},ending:{text:'正在结束营销',tone:'Warning'},unreachable:{text:'不可达',tone:'Danger'},left:{text:'已退群',tone:'Neutral'},
 success:{text:'成功',tone:'Positive'},running:{text:'运行中',tone:'Info'},finished:{text:'已完成',tone:'Positive'},failed:{text:'失败',tone:'Danger'},blocked:{text:'审计阻塞',tone:'Danger'},cancelled:{text:'已取消',tone:'Neutral'},stopped:{text:'已停止',tone:'Warning'},
 queued:{text:'排队中',tone:'Neutral'},pending:{text:'待执行',tone:'Neutral'},accepted:{text:'已受理',tone:'Info'},sent:{text:'已发出',tone:'Positive'},unknown:{text:'结果待确认',tone:'Warning'},skipped:{text:'已跳过',tone:'Neutral'}
}
export default function StatusTag({status}:{status:string}){const config=statuses[status];return <Tag className={styles.status+' '+styles['status'+(config?.tone??'Neutral')]}>{config?.text??status}</Tag>}
