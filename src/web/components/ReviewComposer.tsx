/** @module ReviewComposer @description Writes a review note attached to the session or one prompt; it is an annotation, never a message to the source agent. */
import React,{useState} from "react";
import {excerpt} from "../format";
import type {Turn} from "../types";
import {Icon} from "./Icon";

type Props={
  turns:Turn[];
  sourceLabel:string;
  target:string;
  draft:string;
  textarea:React.RefObject<HTMLTextAreaElement|null>;
  onTarget:(turnID:string)=>void;
  onDraft:(text:string)=>void;
  onSubmit:(body:string,turnID?:string)=>Promise<void>;
};

export function ReviewComposer({turns,sourceLabel,target,draft,textarea,onTarget,onDraft,onSubmit}:Props){
  const[busy,setBusy]=useState(false);
  const[error,setError]=useState("");
  const submit=async(event?:React.FormEvent)=>{
    event?.preventDefault();
    const body=draft.trim();
    if(!body){setError("Write a review note first.");textarea.current?.focus();return}
    setBusy(true);setError("");
    try{await onSubmit(body,target||undefined);onDraft("")}
    catch(value){setError(value instanceof Error?value.message:String(value))}
    finally{setBusy(false)}
  };
  return <div className="composer-wrap">
    <form className="composer" aria-label="Review note" onSubmit={submit}>
      <div className="composer-top"><Icon name="note"/><span>Review note</span><span aria-hidden="true">→</span>
        <select aria-label="Attach note to" value={target} onChange={event=>onTarget(event.target.value)}><option value="">Entire session</option>{turns.map(turn=><option key={turn.id} value={turn.id}>Prompt {turn.sequence} · {excerpt(turn.user_prompt_redacted,32)||"redacted"}</option>)}</select>
      </div>
      <textarea ref={textarea} aria-label="Review note text" placeholder="Leave a note on this session…" maxLength={2000} value={draft} onChange={event=>{onDraft(event.target.value);if(error)setError("")}} onKeyDown={event=>{if((event.ctrlKey||event.metaKey)&&event.key==="Enter"){event.preventDefault();void submit()}}}/>
      {error&&<div className="composer-error" role="alert">{error}</div>}
      <div className="composer-bottom"><span>Annotation only. Not sent to {sourceLabel}.</span><button className="button primary" type="submit" disabled={busy}>{busy?"Adding…":"Add note"}<Icon name="send"/></button></div>
    </form>
  </div>;
}
