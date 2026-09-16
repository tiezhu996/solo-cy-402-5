import { Tag, Tooltip } from 'antd'
import { ConflictStatusColor, ConflictStatusText } from '@/constants/conflict'

// 利益冲突结论状态徽标；失效/命中时额外提示原因，被列表页与提交结果共用。
export default function ConflictStatusBadge({
  status,
  reason,
}: {
  status: string
  reason?: string
}) {
  const tag = <Tag color={ConflictStatusColor[status] || 'default'}>{ConflictStatusText[status] || status}</Tag>
  if (status === 'invalidated' && reason) {
    return <Tooltip title={reason}>{tag}</Tooltip>
  }
  return tag
}
