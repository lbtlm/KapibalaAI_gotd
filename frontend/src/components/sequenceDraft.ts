import type {Sequence,SequenceInput} from '../types'

export const variableKeys=(sequence:Pick<Sequence,'steps'>)=>[...new Set(sequence.steps.flatMap(s=>[...s.text.matchAll(/\{([A-Za-z0-9_]+)\}/g)].map(m=>m[1])))]
export function parseStepVars(text:string):SequenceInput['stepVars']{
 const value:unknown=JSON.parse(text||'{}')
 if(!value||typeof value!=='object'||Array.isArray(value)||!Object.entries(value).every(([key,v])=>/^[1-9]\d*$/.test(key)&&v&&typeof v==='object'&&!Array.isArray(v)&&Object.values(v).every(x=>typeof x==='string')))throw new Error('逐步变量需使用步骤编号和文字值，例如 {"2":{"event":"自由交流"}}。')
 return value as SequenceInput['stepVars']
}
export function resolveDraft(sequence:Sequence,vars:Record<string,string>,overrides:SequenceInput['stepVars']){
 const current={...vars},missing:{index:number;key:string}[]=[]
 const steps=sequence.steps.map(step=>{
  for(const [key,value] of Object.entries(overrides[String(step.index)]??{}))if(value!=='')current[key]=value
  return {...step,text:step.text.replace(/\{([A-Za-z0-9_]+)\}/g,(all,key:string)=>{if(current[key])return current[key];missing.push({index:step.index,key});return all})}
 })
 return {value:{...sequence,steps},missing}
}
