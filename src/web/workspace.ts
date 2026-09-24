/** @module Workspace @description Shared selection shapes that tie the conversation, lenses and inspector to one session context. */
import type {SessionView} from "./navigation";

export type InspectorTab="context"|"evidence"|"notes";

/** What the inspector explains: one call, one prompt, one file or one finding. */
export type Evidence=
  |{kind:"tool";id:string}
  |{kind:"turn";id:string}
  |{kind:"file";path:string}
  |{kind:"finding";id:string};

export type WorkspaceActions={
  selectEvidence:(evidence:Evidence)=>void;
  /** Opens the conversation at a prompt, loading earlier history if needed. */
  jumpToTurn:(turnID:string)=>void;
  /** Focuses the review composer, attached to a prompt or to the whole session. */
  noteOn:(turnID?:string)=>void;
  setView:(view:SessionView)=>void;
};

export function sameEvidence(a:Evidence|undefined,b:Evidence):boolean{
  if(!a||a.kind!==b.kind)return false;
  return a.kind==="file"?a.path===(b as {path:string}).path:a.id===(b as {id:string}).id;
}
