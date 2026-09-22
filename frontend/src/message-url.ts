// Match the native service's deliberately narrow external-link policy.
export function messageURL(value: string): string {
 try {
  const url=new URL(value);
  if(!['https:','http:'].includes(url.protocol)||!url.hostname||url.username||url.password)return '';
  return url.href;
 } catch { return ''; }
}
