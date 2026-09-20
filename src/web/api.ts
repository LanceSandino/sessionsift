/** @module ApiClient @description Fetches local-only SessionSift JSON resources. */
import type { ComparePayload, Session } from "./types";

async function request<T>(path:string):Promise<T>{
  const response=await fetch(path,{headers:{Accept:"application/json"}});
  if(!response.ok) throw new Error(`Local API returned ${response.status}`);
  return response.json() as Promise<T>;
}
export const api={
  sessions:(includeInternal=false)=>request<Session[]>(`/api/v1/sessions${includeInternal?"?include_internal=true":""}`),
  session:(id:string)=>request<Session>(`/api/v1/sessions/${encodeURIComponent(id)}`),
  compare:(a:string,b:string)=>request<ComparePayload>(`/api/v1/compare?a=${encodeURIComponent(a)}&b=${encodeURIComponent(b)}`),
};
