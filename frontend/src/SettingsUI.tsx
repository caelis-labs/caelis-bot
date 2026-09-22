import type { ReactNode } from 'react';

export function SettingGroup({title,children}:{title?:string;children:ReactNode}) {
 return <section className="setting-section">{title&&<h2>{title}</h2>}<div className="setting-list">{children}</div></section>;
}
export function SettingRow({label,description,htmlFor,children}:{label:string;description?:ReactNode;htmlFor?:string;children:ReactNode}) {
 return <div className="setting-row"><div className="setting-label">{htmlFor?<label htmlFor={htmlFor}>{label}</label>:<span>{label}</span>}{description&&<p>{description}</p>}</div><div className="setting-control">{children}</div></div>;
}
export function SettingHelp({children}:{children:ReactNode}) {
 return <details className="setting-help"><summary>了解更多</summary><div>{children}</div></details>;
}
