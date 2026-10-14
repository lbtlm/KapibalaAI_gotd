import styles from '../layout/MainLayout.module.scss'
export default function VariableValues({values,sources}:{values:Record<string,string>;sources:Record<string,string>}){
 const entries=Object.entries(values??{})
 return entries.length?<table className={styles.varTable}><thead><tr><th>变量</th><th>最终值</th><th>来源</th></tr></thead><tbody>{entries.map(([key,value])=><tr key={key}><td><code>{key}</code></td><td>{value}</td><td><code className={styles.varSource}>{sources[key]}</code></td></tr>)}</tbody></table>:<span className={styles.meta}>无变量</span>
}
