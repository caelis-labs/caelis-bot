// Local operation feedback is presentation only: never a conversation item or
// an approval. IDs keep a dismissed/expired notice from clearing its successor.
export type BubbleNotice = {id:number;message:string;pending:boolean};
export type BubbleNoticeAction =
 | {type:'show';notice:BubbleNotice}
 | {type:'clearPending'}
 | {type:'dismiss';id:number};

export function reduceBubbleNotice(current:BubbleNotice|null,action:BubbleNoticeAction):BubbleNotice|null {
 if(action.type==='show')return action.notice;
 if(action.type==='clearPending')return current?.pending?null:current;
 return current?.id===action.id?null:current;
}

export function bubblePresentation(content:string,wanted:boolean,attention:boolean,notice:BubbleNotice|null) {
 // Approval/connection/recovery attention always retains the existing surface.
 const showNotice=!!notice&&!attention;
 return {content:showNotice?notice.message:content,wanted:showNotice||wanted,showNotice};
}
