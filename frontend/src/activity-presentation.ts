import type { Activity } from './backend/contract';
import type { MessageKey } from './i18n/catalogs';

const labels:Record<string,MessageKey>={
 read:'chat.activityRead',edit:'chat.activityEdit',search:'chat.activitySearch',list:'chat.activityList',
 web:'chat.activityWeb',fetch:'chat.activityFetch',execute:'chat.activityExecute',tool:'chat.activityTool',
 delegate:'chat.activityDelegate',image:'chat.activityImage',compact:'chat.activityCompact',plan:'chat.activityPlan',
};
export function activityLabel(activity:Activity,t:(key:MessageKey)=>string) {
 const label=t(labels[activity.kind]??'chat.activityTool');
 return activity.target ? `${label} · ${activity.target}` : label;
}
