export type MenuPlugin={id:string;name:string;description:string;source:'bot'|'codex'};
export function pluginReferenceId(source:MenuPlugin['source'],id:string):string { return `${source}-plugin:${id}`; }

type BotPlugin={id:string;title:string;description:string;installed:boolean;enabled:boolean;status:string};
type NativePlugin={id:string;name:string;description:string;source:string};

// A configured package is available to the resident Bot independently of
// whether it contributes a Skill, an MCP server, or both.
export function botMenuPlugins(items:BotPlugin[]):MenuPlugin[] {
 return items.filter(item=>item.installed&&item.enabled&&['enabled','ready','update_available'].includes(item.status))
  .map(item=>({id:pluginReferenceId('bot',item.id),name:item.title,description:item.description,source:'bot'}));
}

export function nativeMenuPlugins(items:NativePlugin[]):MenuPlugin[] {
 const seen=new Set<string>();
 return items.filter(item=>item.source==='codex'&&!!item.id&&!!item.name&&(!seen.has(item.id)&&seen.add(item.id)))
  .map(item=>({id:pluginReferenceId('codex',item.id),name:item.name,description:item.description,source:'codex'}));
}

export function attachmentMenuDescription(description:string) {
 const text=description.replace(/\s+/g,' ').trim();
 const first=text.match(/^.*?[.!?。！？](?=\s|$)/)?.[0]??text;
 const characters=Array.from(first);
 return characters.length>72?characters.slice(0,71).join('').trimEnd()+'…':first;
}
