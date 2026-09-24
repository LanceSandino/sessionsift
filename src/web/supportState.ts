/** @module SupportState @description Pure lifecycle presentation for code-based support sessions. */
import type {SupportSession} from "./types";

/** Remaining time as m:ss (or h:mm:ss), never negative. */
export function countdown(until:string|undefined,now:number):string{
  const target=until?Date.parse(until):Number.NaN;
  if(!Number.isFinite(target)||target<=now)return "0:00";
  const seconds=Math.floor((target-now)/1000);
  const hours=Math.floor(seconds/3600);
  const minutes=Math.floor((seconds%3600)/60);
  const rest=String(seconds%60).padStart(2,"0");
  return hours>0?`${hours}:${String(minutes).padStart(2,"0")}:${rest}`:`${minutes}:${rest}`;
}

/** Live status derived locally so an expiry shows without waiting for a poll. */
export function liveStatus(support:SupportSession,now:number):SupportSession["status"]{
  if(support.status==="ended"||support.status==="expired")return support.status;
  const deadline=support.status==="connected"?support.expires_at:support.code_expires_at;
  return deadline&&Date.parse(deadline)<=now?"expired":support.status;
}

export function supportHeadline(support:SupportSession,now:number):string{
  switch(liveStatus(support,now)){
    case "waiting":return "Waiting for your supporter to enter the code";
    case "connected":return `Connected: ${support.supporter||"supporter"}`;
    case "expired":return support.connected_at?"Support access has expired":"The code expired before it was used";
    default:return endReasonLabel(support.end_reason);
  }
}

export function endReasonLabel(reason:string|undefined):string{
  switch(reason){
    case "supporter_disconnected":return "Your supporter disconnected";
    case "too_many_attempts":return "Ended after too many wrong codes";
    case "bridge_unavailable":return "The bridge could not be reached";
    default:return "You ended this support session";
  }
}

/** The newest support session for a conversation that is still waiting or connected. */
export function activeSupportFor(values:SupportSession[],sessionId:string,now:number):SupportSession|undefined{
  return values
    .filter(value=>value.session_id===sessionId&&(liveStatus(value,now)==="waiting"||liveStatus(value,now)==="connected"))
    .sort((left,right)=>Date.parse(right.created_at)-Date.parse(left.created_at))[0];
}
