/** @module ApiClient @description Fetches local-only SessionSift JSON resources. */
import type { ComparePayload, RemotePairing, RemoteStatus, Session } from "./types";

async function request<T>(path:string):Promise<T>{
  const response=await fetch(path,{headers:{Accept:"application/json"}});
  if(!response.ok) throw new Error(`Local API returned ${response.status}`);
  return response.json() as Promise<T>;
}
async function requestArray<T>(path:string):Promise<T[]>{const value=await request<T[]|null>(path);return Array.isArray(value)?value:[]}
async function send<T>(path:string,body:unknown):Promise<T>{const response=await fetch(path,{method:"POST",headers:{Accept:"application/json","Content-Type":"application/json"},body:JSON.stringify(body)});if(!response.ok)throw new Error(`Local API returned ${response.status}`);return response.json() as Promise<T>}
async function remoteSend<T>(path:string,body:unknown,token:string):Promise<T>{const response=await fetch(path,{method:"POST",headers:{Accept:"application/json","Content-Type":"application/json",Authorization:`Bearer ${token}`},body:JSON.stringify(body)});if(!response.ok)throw new Error(`Remote API returned ${response.status}`);return response.json() as Promise<T>}
export const api={
  sessions:(includeInternal=false)=>requestArray<Session>(`/api/v1/sessions${includeInternal?"?include_internal=true":""}`),
  session:(id:string)=>request<Session>(`/api/v1/sessions/${encodeURIComponent(id)}`),
  compare:(a:string,b:string)=>request<ComparePayload>(`/api/v1/compare?a=${encodeURIComponent(a)}&b=${encodeURIComponent(b)}`),
  remoteStatus:()=>request<RemoteStatus>("/api/v1/remote/status"),
  createPairing:()=>send<RemotePairing>("/api/v1/remote/pairings",{}),
  approvePairing:(code:string)=>send<{grant:unknown;token:string}>("/api/v1/remote/pairings/approve",{code,duration_minutes:30}),
  denyPairing:(code:string)=>send("/api/v1/remote/pairings/deny",{code}),
  revokeGrant:(grant_id:string)=>send("/api/v1/remote/grants/revoke",{grant_id}),
  disconnect:(connection_id:string)=>send("/api/v1/remote/connections/disconnect",{connection_id}),
  annotations:(sessionId:string)=>requestArray<import("./types").Annotation>(`/api/v1/remote/annotations?session_id=${encodeURIComponent(sessionId)}`),
  addAnnotation:(value:{session_id:string;turn_id?:string;author:string;body:string})=>send<import("./types").Annotation>("/api/v1/remote/annotations",value),
  reviews:(sessionId:string)=>requestArray<import("./types").Review>(`/api/v1/remote/reviews?session_id=${encodeURIComponent(sessionId)}`),
  addReview:(value:{session_id:string;reviewer:string;status:string;summary:string})=>send<import("./types").Review>("/api/v1/remote/reviews",value),
  claimPairing:(code:string,reviewer:string)=>send<RemotePairing>("/remote/v1/pairings/claim",{code,reviewer,operations:["session.list","session.get","session.timeline","session.findings","annotation.list","annotation.create","review.list","review.create"]}),
  remoteProtocol:<T>(token:string,operation:string,args:Record<string,unknown>={})=>remoteSend<{status:string;data:T;error?:{message:string}}>("/remote/v1/protocol",{version:1,request_id:`web_${Date.now()}`,operation,arguments:args},token),
};
