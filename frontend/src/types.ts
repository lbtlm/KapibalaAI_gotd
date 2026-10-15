export type AccountStatus = 'idle' | 'online' | 'rate_limited' | 'disconnected' | 'suspended' | 'session_expired'
export interface Account { id: string; status: AccountStatus; platformUserId: string | null; rateLimitedUntil: string | null; phone?: string | null; phoneVerified?: boolean; verificationSource?: 'telegram' | 'simulated' | null; username?: string | null; firstName?: string | null; lastName?: string | null; groupId?: string | null; lastCheckedAt?: string | null; createdAt?: string; premium?: boolean | null; lastCheckResult?: 'success' | 'failed' | null; lastCheckError?: string | null }
export interface AccountGroup { id: string; name: string; accountCount: number; createdAt: string }
export interface JoinedGroup { id: string; title: string; username: string | null; type: 'group' | 'supergroup'; role: 'creator' | 'admin' | 'member' | 'unknown' }
export interface JoinedGroups { items: JoinedGroup[]; lastSyncedAt: string | null }
export interface AccountLog { id: string; actor: string; action: string; result: 'success' | 'failed'; errorCode: string | null; createdAt: string }
export interface AccountLogPage { items: AccountLog[]; nextCursor: string | null }
export interface AccountImportResult { basename: string; accountId: string | null; success: boolean; error: {code:string;message:string} | null }
export interface Member { accountId: string | null; platformUserId: string; role: 'creator' | 'admin' | 'member' }
export interface Group { id: string; gatewayGroupId: string; status: 'active' | 'unreachable' | 'left'; creatorAccountId: string | null; agentEnabled: boolean; autoKickEnabled: boolean; members: Member[]; activeSequenceRunId: string | null; activeAgentRunId: string | null }
export interface Message { msgId: string | null; clientMsgId: string | null; senderPlatformUserId: string; isOwn: boolean; text: string; sentAt: string; deliveryStatus: string | null; failCode: string | null }
export interface MessagePage { items: Message[]; nextCursor: string | null }
export interface AgentStep { kind: string; toolUseId: string | null; name: string | null; input: unknown; resultSummary: string; isError: boolean; errorCode: string | null; auditVerdict: string | null; rawResponse: string }
export interface AgentRun { id: string; groupId: string; status: string; endReason: string | null; summary: string | null; steps?: AgentStep[] }
export interface Sequence { id: string; name: string; steps: { index: number; accountRole: 'admin' | 'member'; text: string; delaySeconds: number }[] }
export interface SequenceInput { sequenceId: string; vars: Record<string,string>; stepVars: Record<string,Record<string,string>> }
export interface PreviewStep { index: number; text: string; resolvedVars: Record<string,string>; varSources: Record<string,string> }
export interface SequenceRun { status: string; currentStepIndex: number; steps: (PreviewStep & { status: string; scheduledAt: string | null; sentAt: string | null; clientMsgId: string | null })[] }
export interface Job { status: 'running' | 'finished' | 'failed'; errors: { step: string; code: string }[] }


