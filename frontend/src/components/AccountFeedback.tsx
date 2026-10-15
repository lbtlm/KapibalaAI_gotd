import type {ApiError} from '../api/client'
import {ErrorNotice} from './Feedback'
const messages:Record<string,string>={
 ACCOUNT_ALREADY_IMPORTED:'账号已导入，请勿重复上传',ACCOUNT_IMPORT_CONFLICT:'账号授权或配置冲突，未覆盖已有账号',AMBIGUOUS_FILE_PAIR:'同名文件重复，无法唯一配对',INCOMPLETE_FILE_PAIR:'缺少同名的 Session 或 JSON 文件',CONFLICTING_API_CREDENTIALS:'JSON 中的 API 凭据相互冲突',INVALID_API_CREDENTIALS:'API 凭据格式不正确',INVALID_METADATA:'JSON 资料格式不正确',INVALID_MULTIPART:'上传格式不正确，请重新选择文件',INVALID_SESSION:'Session 文件无效或格式不受支持',NO_FILES:'请选择账号文件',TOO_MANY_FILES:'本批次文件过多，请分批导入',UNSUPPORTED_FILE:'仅支持 .session 与 .json 文件',ACCOUNT_GROUP_NOT_FOUND:'分组已不存在，请刷新后重试',ACCOUNT_GROUP_NAME_EXISTS:'分组名称已存在',ACCOUNT_NOT_FOUND:'账号已不存在，请刷新列表',ACCOUNT_OFFLINE:'账号未在线，请先连接并检测',ACCOUNT_SUSPENDED:'账号已停用，无法重新连接',SESSION_EXPIRED:'会话已失效，无法重新连接',ILLEGAL_TRANSITION:'当前账号状态不允许此操作',CAS_CONFLICT:'账号状态已变化，请刷新后重试',RATE_LIMITED:'账号受到限流，请等待恢复',CONNECTION_ENDED:'账号连接已断开',GATEWAY_UNAVAILABLE:'连接服务暂不可用，请稍后重试',GATEWAY_PROTOCOL_ERROR:'连接服务返回异常，请稍后重试',GATEWAY_ERROR:'连接服务操作失败，请稍后重试'
}
export const accountErrorText=(code:string,fallback?:string)=>messages[code]??fallback??'账号操作失败，请稍后重试'
export function AccountErrorNotice({error}:{error:unknown}){if(!error)return null;const e=error as ApiError;return <ErrorNotice error={{...e,message:accountErrorText(e.code,e.message)}}/>}
