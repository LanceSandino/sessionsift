/** @module ApiClient @description Fetches local-only SessionSift JSON resources. */
import type { AnalyticsReport, BridgeStatus, ChangelogPage, CloudSyncStatus, ComparePayload, DiagnosticsStatus, HostedInvitation, LinkOffer, OptimizationExperiment, OptimizationOverview, RecommendationState, RecoveryOffer, RemoteGrant, RemotePairing, RemoteStatus, Session, SupportOffer, SupportRole, SupportSession, SyncConflict } from "./types";

async function request<T>(path:string):Promise<T>{
  const response=await fetch(path,{headers:{Accept:"application/json"}});
  if(!response.ok) throw new Error(`Local API returned ${response.status}`);
  return response.json() as Promise<T>;
}
async function requestArray<T>(path:string):Promise<T[]>{const value=await request<T[]|null>(path);return Array.isArray(value)?value:[]}
async function send<T>(path:string,body:unknown):Promise<T>{const response=await fetch(path,{method:"POST",headers:{Accept:"application/json","Content-Type":"application/json"},body:JSON.stringify(body)});if(!response.ok){const detail=await response.json().catch(()=>undefined) as {error?:string}|undefined;throw new Error(detail?.error||`Local API returned ${response.status}`)}return response.json() as Promise<T>}
async function remoteSend<T>(path:string,body:unknown,token:string):Promise<T>{const response=await fetch(path,{method:"POST",headers:{Accept:"application/json","Content-Type":"application/json",Authorization:`Bearer ${token}`},body:JSON.stringify(body)});if(!response.ok)throw new Error(`Remote API returned ${response.status}`);return response.json() as Promise<T>}
export const api={
  sessions:async(includeInternal=false)=>{
    const items:Session[]=[];
    for(let cursor=0;;cursor+=100){const page=await requestArray<Session>(`/api/v1/sessions?limit=100&cursor=${cursor}&include_internal=${includeInternal}`);items.push(...page);if(page.length<100)return items}
  },
  session:(id:string)=>request<Session>(`/api/v1/sessions/${encodeURIComponent(id)}`),
  compare:(a:string,b:string)=>request<ComparePayload>(`/api/v1/compare?a=${encodeURIComponent(a)}&b=${encodeURIComponent(b)}`),
  analytics:()=>request<AnalyticsReport>("/api/v1/analytics"),
  optimization:()=>request<OptimizationOverview>("/api/v1/optimization"),
  createExperiment:(value:{title:string;hypothesis?:string;recommendation_id?:string;baseline_session_id:string;followup_session_id?:string})=>send<OptimizationExperiment>("/api/v1/optimization/experiments",value),
  updateExperiment:(value:{id:string;title:string;hypothesis?:string;recommendation_id?:string;baseline_session_id:string;followup_session_id?:string})=>send<OptimizationExperiment>("/api/v1/optimization/experiments/update",value),
  setRecommendation:(recommendation_id:string,state:RecommendationState)=>send("/api/v1/optimization/recommendations",{recommendation_id,state}),
  recordAdoption:(event:"optimize_viewed")=>send("/api/v1/optimization/adoption",{event}),
  diagnostics:()=>request<DiagnosticsStatus>("/api/v1/diagnostics"),
  changelog:(page=1,pageSize=5)=>request<ChangelogPage>(`/api/v1/changelog?page=${page}&page_size=${pageSize}`),
  syncStatus:()=>request<CloudSyncStatus>("/api/v1/sync"),
  prepareSync:(selection:{summaries:boolean;findings_notes:boolean;detailed_evidence:boolean})=>send<RecoveryOffer>("/api/v1/sync/prepare",selection),
  enableSync:(recovery_secret:string,selection:{summaries:boolean;findings_notes:boolean;detailed_evidence:boolean})=>send<CloudSyncStatus>("/api/v1/sync/enable",{recovery_secret,...selection}),
  recoverSync:(recovery_secret:string,selection:{summaries:boolean;findings_notes:boolean;detailed_evidence:boolean})=>send<CloudSyncStatus>("/api/v1/sync/recover",{recovery_secret,...selection}),
  runSync:()=>send<{queued:{examined:number;queued:number;bytes:number}}>("/api/v1/sync/run",{}),
  pauseSync:()=>send("/api/v1/sync/pause",{}),
  resumeSync:()=>send("/api/v1/sync/resume",{}),
  disableSync:()=>send("/api/v1/sync/disable",{}),
  syncConflicts:()=>requestArray<SyncConflict>("/api/v1/sync/conflicts"),
  resolveSyncConflict:(id:number,resolution:"local"|"remote")=>send("/api/v1/sync/conflicts/resolve",{id,resolution}),
  deleteCloudSync:(recovery_secret:string)=>send("/api/v1/sync/delete-cloud",{recovery_secret,confirmation:"DELETE CLOUD COPIES"}),
  removeLocalSync:(recovery_secret:string)=>send("/api/v1/sync/remove-local",{recovery_secret,confirmation:"REMOVE LOCAL SYNC DATA"}),
  rotateSyncKey:(recovery_secret:string)=>send<RecoveryOffer>("/api/v1/sync/rotate-key",{recovery_secret,confirmation:"ROTATE SYNC KEY"}),
  remoteStatus:()=>request<RemoteStatus>("/api/v1/remote/status"),
  bridge:()=>request<BridgeStatus>("/api/v1/remote/bridge"),
  connectBridge:(url:string,name:string)=>send<BridgeStatus>("/api/v1/remote/bridge/connect",{url,name}),
  startLink:()=>send<LinkOffer>("/api/v1/remote/bridge/link",{}),
  refreshBridge:()=>send<BridgeStatus>("/api/v1/remote/bridge/refresh",{}),
  supportSessions:()=>requestArray<SupportSession>("/api/v1/remote/support"),
  startSupport:(session_id:string,role:SupportRole,minutes:number)=>send<SupportOffer>("/api/v1/remote/support",{session_id,role,minutes}),
  endSupport:(id:string)=>send<SupportSession>("/api/v1/remote/support/end",{id}),
  approveOAuthPrincipal:(principal:string,detailed=false)=>send<RemoteGrant>("/api/v1/remote/oauth/grants",{principal,duration_days:14,operations:detailed?["session.list","session.get","session.metrics","session.timeline","session.findings","session.compare","turn.get","analysis.patterns","analysis.recurring_corrections","context.recommendations"]:["session.list","analysis.patterns","analysis.recurring_corrections","context.recommendations"]}),
  createPairing:()=>send<RemotePairing>("/api/v1/remote/pairings",{}),
  approvePairing:(code:string)=>send<{grant:unknown;token:string}>("/api/v1/remote/pairings/approve",{code,duration_minutes:30}),
  denyPairing:(code:string)=>send("/api/v1/remote/pairings/deny",{code}),
  revokeGrant:(grant_id:string)=>send("/api/v1/remote/grants/revoke",{grant_id}),
  createInvitation:(value:{invitee:string;session_id:string;role:"view"|"comment"|"review";duration_hours:number})=>send<HostedInvitation>("/api/v1/remote/invitations",value),
  invitations:()=>requestArray<HostedInvitation>("/api/v1/remote/invitations"),
  revokeInvitation:(invitation_id:string)=>send("/api/v1/remote/invitations/revoke",{invitation_id}),
  disconnect:(connection_id:string)=>send("/api/v1/remote/connections/disconnect",{connection_id}),
  annotations:(sessionId:string)=>requestArray<import("./types").Annotation>(`/api/v1/remote/annotations?session_id=${encodeURIComponent(sessionId)}`),
  addAnnotation:(value:{session_id:string;turn_id?:string;author:string;body:string})=>send<import("./types").Annotation>("/api/v1/remote/annotations",value),
  reviews:(sessionId:string)=>requestArray<import("./types").Review>(`/api/v1/remote/reviews?session_id=${encodeURIComponent(sessionId)}`),
  addReview:(value:{session_id:string;reviewer:string;status:string;summary:string})=>send<import("./types").Review>("/api/v1/remote/reviews",value),
  claimPairing:(code:string,reviewer:string)=>send<RemotePairing>("/remote/v1/pairings/claim",{code,reviewer,operations:["session.list","session.get","session.timeline","session.findings","annotation.list","annotation.create","review.list","review.create"]}),
  remoteProtocol:<T>(token:string,operation:string,args:Record<string,unknown>={})=>remoteSend<{status:string;data:T;error?:{message:string}}>("/remote/v1/protocol",{version:1,request_id:`web_${Date.now()}`,operation,arguments:args},token),
};
