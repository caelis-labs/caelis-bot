// Independent integration contract; the shared settings/transport owner can
// consume it without changing chat, cancellation or split-panel components.
export interface ManagedUpdateStatus {
 protocol:1;
 mode:'managed'|'external'|'image';
 state:string;
 reason?:string;
 current:{version:string;source:string;kind:string};
 bootId?:string;
 offerId?:string;
 latest?:string;
 transactionId?:string;
 revision:number;
 receivedBytes:number;
 totalBytes:number;
 automatic:boolean;
 scheduled:boolean;
 testArtifact:boolean;
 runtime?:{state:string;reason?:string;bundledVersion?:string;hostVersion?:string;hostBuild?:string;hostInstance?:string};
}
export interface ManagedUpdateCommand {
 id:string; // retain original ID across timeout/reload
 action:'check'|'download'|'install'|'cancel'|'automatic';
 offerId?:string;
 transactionId?:string;
 revision?:number;
 when?:'now'|'idle';
 automatic?:boolean;
}
export interface ManagedUpdateReceipt {
 id:string;
 transactionId?:string;
 state:'accepted'|'read_only'; // does not prove download/install/health
}
export const managedUpdateRoutes = {
 status:'/api/v1/updates/status',
 command:'/api/v1/updates/command',
 receipt:(id:string)=>'/api/v1/updates/receipts/'+encodeURIComponent(id),
} as const;
