import { useEffect, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Col,
  Descriptions,
  Form,
  Input,
  Modal,
  Row,
  Select,
  Space,
  Table,
  Tag,
  Typography,
  message,
} from 'antd'
import { PlusOutlined, SearchOutlined } from '@ant-design/icons'
import {
  getConflict,
  listConflicts,
  lookupConflict,
  rejectConflict,
  releaseConflict,
  submitConflict,
} from '@/api/conflict'
import { useAuthStore } from '@/stores/authStore'
import ConflictStatusBadge from '@/components/common/ConflictStatusBadge'
import {
  ConflictStatus,
  ConflictStatusOptions,
  MatchedByText,
  PartySideText,
} from '@/constants/conflict'
import { CaseStatusText } from '@/constants/case'
import type { ConflictCheck, PartySnapshot } from '@/types'

const { TextArea } = Input
const { Text, Title } = Typography

// 生成新案业务幂等键：同一新案重复提交复用它以收口到一条结论；登记另一个新案时重新生成。
function genCaseKey() {
  return 'NEW-' + Date.now().toString(36).toUpperCase() + '-' + Math.random().toString(36).slice(2, 8).toUpperCase()
}

export default function ConflictChecks() {
  const isAdmin = useAuthStore((s) => s.user?.role === 'admin')
  const [submitForm] = Form.useForm()
  const [result, setResult] = useState<ConflictCheck | null>(null)
  const [caseKey, setCaseKey] = useState(genCaseKey())

  // 复核队列
  const [list, setList] = useState<ConflictCheck[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [statusFilter, setStatusFilter] = useState<string>(ConflictStatus.PENDING_REVIEW)

  // 同入口读回：优先按新案编号精确读回；仅填对方姓名/证件号时返回该对方最近一条。
  const [lookupCaseKey, setLookupCaseKey] = useState('')
  const [lookupName, setLookupName] = useState('')
  const [lookupID, setLookupID] = useState('')

  // 复核弹窗
  const [reviewTarget, setReviewTarget] = useState<ConflictCheck | null>(null)
  const [reviewAction, setReviewAction] = useState<'release' | 'reject'>('release')
  const [reviewForm] = Form.useForm()

  async function fetchList() {
    const res: any = await listConflicts({ page, page_size: pageSize, status: statusFilter })
    setList(res.data.list)
    setTotal(res.data.total)
  }

  useEffect(() => {
    fetchList()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, pageSize, statusFilter])

  async function refreshResult(id: number) {
    const res: any = await getConflict(id)
    setResult(res.data)
  }

  async function onSubmit() {
    const v = await submitForm.validateFields()
    const ourParties = (v.our_parties || []).filter((p: any) => p && p.name && p.name.trim())
    if (ourParties.length === 0) {
      message.error('请至少填写一名本方客户或联系人')
      return
    }
    const res: any = await submitConflict({
      case_key: caseKey,
      case_title: v.case_title,
      our_parties: ourParties,
      opp_name: v.opp_name,
      opp_id_number: v.opp_id_number,
    })
    const chk: ConflictCheck = res.data
    setResult(chk)
    setLookupCaseKey(chk.case_key)
    setLookupName(chk.opp_name)
    setLookupID(chk.opp_id_number)
    message.success(res.message)
    setPage(1)
    fetchList()
  }

  // 登记另一个新案：生成新的新案键并清空表单，确保与上一个新案各自独立。
  function startAnotherCase() {
    const next = genCaseKey()
    setCaseKey(next)
    setLookupCaseKey(next)
    submitForm.resetFields()
    setResult(null)
  }

  async function onLookup() {
    if (!lookupCaseKey.trim() && !lookupName.trim()) {
      message.warning('请输入新案编号或对方姓名后再查询')
      return
    }
    const res: any = await lookupConflict({
      case_key: lookupCaseKey.trim() || undefined,
      opp_name: lookupName.trim() || undefined,
      opp_id_number: lookupID.trim() || undefined,
    })
    setResult(res.data)
  }

  function openReview(row: ConflictCheck, action: 'release' | 'reject') {
    setReviewTarget(row)
    setReviewAction(action)
    reviewForm.resetFields()
  }

  async function onConfirmReview() {
    if (!reviewTarget) return
    const { basis } = await reviewForm.validateFields()
    if (reviewAction === 'release') {
      await releaseConflict(reviewTarget.id, basis)
      message.success('已放行')
    } else {
      await rejectConflict(reviewTarget.id, basis)
      message.success('已驳回')
    }
    setReviewTarget(null)
    await refreshResult(reviewTarget.id).catch(() => {})
    fetchList()
  }

  const columns = [
    { title: '单号', dataIndex: 'check_no', width: 150 },
    { title: '新案编号', dataIndex: 'case_key', width: 170, ellipsis: true },
    { title: '新案名称', dataIndex: 'case_title', ellipsis: true },
    { title: '对方姓名', dataIndex: 'opp_name', width: 110 },
    { title: '对方证件号', dataIndex: 'opp_id_number', width: 190 },
    {
      title: '命中',
      dataIndex: 'hit_count',
      width: 70,
      render: (n: number) => <Tag color={n > 0 ? 'orange' : 'green'}>{n}</Tag>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 120,
      render: (s: string, row: ConflictCheck) => <ConflictStatusBadge status={s} reason={row.invalidated_reason} />,
    },
    { title: '提交人', dataIndex: 'submit_by_name', width: 100 },
    {
      title: '操作',
      width: 200,
      render: (_: unknown, row: ConflictCheck) => (
        <Space>
          <Button type="link" onClick={() => setResult(row)}>
            结论
          </Button>
          {isAdmin && row.status === ConflictStatus.PENDING_REVIEW && (
            <>
              <Button type="link" onClick={() => openReview(row, 'release')}>
                放行
              </Button>
              <Button type="link" danger onClick={() => openReview(row, 'reject')}>
                驳回
              </Button>
            </>
          )}
        </Space>
      ),
    },
  ]

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Row gutter={16}>
        <Col xs={24} lg={11}>
          <Card title="新案提交 · 案源利益冲突检查" size="small">
            <Form form={submitForm} layout="vertical" initialValues={{ our_parties: [{}] }}>
              <Form.Item label="新案编号（同一新案重复提交保持一致，登记另一新案请更换）">
                <Space.Compact style={{ width: '100%' }}>
                  <Input value={caseKey} onChange={(e) => setCaseKey(e.target.value)} placeholder="新案编号" />
                  <Button onClick={startAnotherCase}>登记另一个新案</Button>
                </Space.Compact>
              </Form.Item>
              <Form.Item name="case_title" label="新案名称" rules={[{ required: true, message: '请输入新案名称' }]}>
                <Input placeholder="例如：与广州恒达物流的货款纠纷" />
              </Form.Item>

              <Form.Item label="本方（客户及联系人，至少一人）" required>
                <Form.List name="our_parties">
                  {(fields, { add, remove }) => (
                    <Space direction="vertical" style={{ width: '100%' }}>
                      {fields.map((field) => (
                        <Space key={field.key} align="baseline">
                          <Form.Item name={[field.name, 'name']} rules={[{ required: true, message: '姓名必填' }]}>
                            <Input placeholder="本方姓名" />
                          </Form.Item>
                          <Form.Item name={[field.name, 'id_number']}>
                            <Input placeholder="证件号" />
                          </Form.Item>
                          <Form.Item name={[field.name, 'contact']}>
                            <Input placeholder="联系方式" />
                          </Form.Item>
                          {fields.length > 1 && (
                            <Button type="link" danger onClick={() => remove(field.name)}>
                              移除
                            </Button>
                          )}
                        </Space>
                      ))}
                      <Button type="dashed" icon={<PlusOutlined />} onClick={() => add({})} block>
                        添加本方联系人
                      </Button>
                    </Space>
                  )}
                </Form.List>
              </Form.Item>

              <Row gutter={12}>
                <Col span={10}>
                  <Form.Item name="opp_name" label="对方姓名" rules={[{ required: true, message: '请填写对方姓名' }]}>
                    <Input placeholder="对方姓名" />
                  </Form.Item>
                </Col>
                <Col span={14}>
                  <Form.Item name="opp_id_number" label="对方证件号（选填，优先用于命中）">
                    <Input placeholder="对方证件号" />
                  </Form.Item>
                </Col>
              </Row>

              <Button type="primary" block onClick={onSubmit}>
                提交并检查冲突
              </Button>
            </Form>
          </Card>
        </Col>

        <Col xs={24} lg={13}>
          <Card
            title="结论读回（同一入口）"
            size="small"
            extra={
              <Space wrap>
                <Input
                  allowClear
                  placeholder="新案编号（精确）"
                  value={lookupCaseKey}
                  onChange={(e) => setLookupCaseKey(e.target.value)}
                  style={{ width: 170 }}
                />
                <Input
                  allowClear
                  placeholder="对方姓名"
                  value={lookupName}
                  onChange={(e) => setLookupName(e.target.value)}
                  style={{ width: 120 }}
                />
                <Input
                  allowClear
                  placeholder="对方证件号"
                  value={lookupID}
                  onChange={(e) => setLookupID(e.target.value)}
                  style={{ width: 160 }}
                />
                <Button icon={<SearchOutlined />} onClick={onLookup}>
                  读回
                </Button>
              </Space>
            }
          >
            {!result ? (
              <Text type="secondary">
                按「新案编号」精确读回该案结论；仅填对方姓名/证件号时返回该对方最近一条（同一对方可能对应多个不同新案）。
              </Text>
            ) : (
              <ResultPanel check={result} isAdmin={isAdmin} onReview={openReview} />
            )}
          </Card>
        </Col>
      </Row>

      <Card
        title={isAdmin ? '冲突复核队列' : '冲突检查记录'}
        size="small"
        extra={
          <Select
            value={statusFilter}
            style={{ width: 160 }}
            options={ConflictStatusOptions}
            onChange={(v) => {
              setStatusFilter(v)
              setPage(1)
            }}
            allowClear
            placeholder="全部状态"
          />
        }
      >
        <Table<ConflictCheck>
          rowKey="id"
          dataSource={list}
          columns={columns}
          pagination={{
            current: page,
            pageSize,
            total,
            showSizeChanger: true,
            onChange: (p, ps) => {
              setPage(p)
              setPageSize(ps)
            },
          }}
        />
      </Card>

      <Modal
        title={reviewAction === 'release' ? '管理员放行（须填写依据）' : '管理员驳回（须填写依据）'}
        open={!!reviewTarget}
        onOk={onConfirmReview}
        onCancel={() => setReviewTarget(null)}
        okText={reviewAction === 'release' ? '确认放行' : '确认驳回'}
        okButtonProps={{ danger: reviewAction === 'reject' }}
      >
        {reviewTarget && (
          <Alert
            style={{ marginBottom: 12 }}
            type="warning"
            showIcon
            message={
              <span>
                对方「{reviewTarget.opp_name}」命中 {reviewTarget.hit_count} 条现存未结案件对方档案。放行结论将绑定提交时档案版本，
                档案一旦变化旧放行自动失效。
              </span>
            }
          />
        )}
        <Form form={reviewForm} layout="vertical">
          <Form.Item name="basis" label="审查依据" rules={[{ required: true, min: 2, message: '请填写至少 2 个字的依据' }]}>
            <TextArea rows={4} placeholder="例如：已核实系不同主体 / 已取得冲突豁免书面同意 / 关联案件已结案……" />
          </Form.Item>
        </Form>
      </Modal>
    </Space>
  )
}

function ResultPanel({
  check,
  isAdmin,
  onReview,
}: {
  check: ConflictCheck
  isAdmin: boolean
  onReview: (row: ConflictCheck, action: 'release' | 'reject') => void
}) {
  const canProceed = check.can_proceed
  return (
    <Space direction="vertical" style={{ width: '100%' }} size={12}>
      <Space>
        <ConflictStatusBadge status={check.status} reason={check.invalidated_reason} />
        <Text type="secondary">{check.check_no}</Text>
        {check.stale && <Tag color="default">档案已变化</Tag>}
      </Space>

      {check.status === ConflictStatus.PENDING_REVIEW && (
        <Alert type="warning" showIcon message="命中现存未结案件对方档案，仅保存为待复核；须管理员填写依据后放行，方可办理。" />
      )}
      {check.status === ConflictStatus.RELEASED && !check.stale && (
        <Alert type="success" showIcon message="管理员已放行（绑定当前档案版本），可以办理；若对方档案变化，本放行将自动失效。" />
      )}
      {check.status === ConflictStatus.INVALIDATED && (
        <Alert type="error" showIcon message={check.invalidated_reason || '绑定档案已变化，原放行结论失效，须重新复核。'} />
      )}
      {check.status === ConflictStatus.REJECTED && <Alert type="error" showIcon message="管理员已驳回，该新案不得办理。" />}
      {check.status === ConflictStatus.NO_CONFLICT && !check.live_match && (
        <Alert type="success" showIcon message="未命中现存未结案件对方档案，可正常办理。" />
      )}

      <Descriptions size="small" column={2} bordered>
        <Descriptions.Item label="新案编号" span={2}>
          {check.case_key}
        </Descriptions.Item>
        <Descriptions.Item label="新案名称" span={2}>
          {check.case_title}
        </Descriptions.Item>
        <Descriptions.Item label="对方姓名">{check.opp_name}</Descriptions.Item>
        <Descriptions.Item label="对方证件号">{check.opp_id_number || '—'}</Descriptions.Item>
        <Descriptions.Item label="是否可办理" span={2}>
          <Tag color={canProceed ? 'green' : 'red'}>{canProceed ? '可办理' : '不可办理'}</Tag>
        </Descriptions.Item>
        {check.review_basis && (
          <Descriptions.Item label="复核依据" span={2}>
            {check.review_basis}
            <br />
            <Text type="secondary">
              复核人：{check.review_by_name}
              {check.reviewed_at ? ` · ${new Date(check.reviewed_at).toLocaleString()}` : ''}
            </Text>
          </Descriptions.Item>
        )}
      </Descriptions>

      {check.matched_snapshot?.length > 0 && (
        <>
          <Title level={5} style={{ margin: 0 }}>
            提交时命中的对方档案（绑定版本 v{check.bound_party_version}）
          </Title>
          <Table<PartySnapshot>
            size="small"
            rowKey="party_id"
            pagination={false}
            dataSource={check.matched_snapshot}
            columns={[
              { title: '档案姓名', dataIndex: 'name' },
              { title: '证件号', dataIndex: 'id_number' },
              {
                title: '立场',
                dataIndex: 'side',
                width: 70,
                render: (s: string) => PartySideText[s] || s,
              },
              {
                title: '所在案件',
                render: (_, r) => (
                  <span>
                    {r.case_no} · {r.case_title}
                    <Tag style={{ marginInlineStart: 4 }}>{CaseStatusText[r.case_status] || r.case_status}</Tag>
                  </span>
                ),
              },
              { title: '版本', dataIndex: 'version', width: 60 },
              {
                title: '命中方式',
                dataIndex: 'matched_by',
                width: 100,
                render: (m: string) => MatchedByText[m] || m,
              },
            ]}
          />
        </>
      )}

      {isAdmin && check.status === ConflictStatus.PENDING_REVIEW && (
        <Space>
          <Button type="primary" onClick={() => onReview(check, 'release')}>
            填写依据并放行
          </Button>
          <Button danger onClick={() => onReview(check, 'reject')}>
            填写依据并驳回
          </Button>
        </Space>
      )}
    </Space>
  )
}
