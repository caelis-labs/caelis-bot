export function questionStatusKey(state:string) {
 switch(state){
  case 'answered':case 'accepted':return 'chat.questionAnswered' as const;
  case 'dismissed':return 'chat.questionDismissed' as const;
  case 'expired':return 'chat.questionExpired' as const;
  case 'superseded':return 'chat.questionSuperseded' as const;
  default:return 'chat.questionUnknown' as const;
 }
}
