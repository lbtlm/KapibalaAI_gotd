import { Tag } from 'antd'
const statuses: Record<string, { text: string; color: string }> = {
  idle: { text: '空闲', color: 'default' }, online: { text: '在线', color: 'green' }, rate_limited: { text: '限流中', color: 'gold' },
  disconnected: { text: '已离线', color: 'default' }, suspended: { text: '已停用', color: 'red' }, session_expired: { text: '会话失效', color: 'red' },
  active: { text: '正常', color: 'green' }, unreachable: { text: '不可达', color: 'red' }, left: { text: '已退群', color: 'default' },
  running: { text: '运行中', color: 'blue' }, finished: { text: '已完成', color: 'green' }, failed: { text: '失败', color: 'red' }, blocked: { text: '审计阻塞', color: 'red' }, cancelled: { text: '已取消', color: 'default' }, stopped: { text: '已停止', color: 'gold' },
  queued: { text: '排队中', color: 'default' }, pending: { text: '待执行', color: 'default' }, accepted: { text: '已受理', color: 'blue' }, sent: { text: '已发出', color: 'green' }, unknown: { text: '结果待确认', color: 'gold' }, skipped: { text: '已跳过', color: 'default' }
}
export default function StatusTag({ status }: { status: string }) { const config = statuses[status]; return <Tag color={config?.color}>{config?.text ?? status}</Tag> }

