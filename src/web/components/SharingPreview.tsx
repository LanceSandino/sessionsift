/** @module SharingPreview @description Shows the exact retained content boundary before creating an invitation. */
import React from "react";
import type {Session} from "../types";
import {RichContent} from "./RichContent";

export function SharingPreview({session}:{session:Session}){
 return <details className="sharingPreview"><summary>Review shared content · {session.turns?.length||0} prompts · {session.tool_calls?.length||0} tools</summary>
  <p>Recipients can read the redacted conversation, tool arguments and results, timeline, findings, and metadata for this session—including new content added before the invitation expires. This is live access, not a frozen snapshot. Redaction can miss secrets: inspect sensitive content before sharing.</p>
  <div className="sharingPreviewScroll" tabIndex={0} aria-label="Shared content preview">
   {(session.turns||[]).map(turn=><article key={turn.id}><h4>Prompt {turn.sequence}</h4><RichContent text={turn.user_prompt_redacted||""}/>{turn.assistant_summary_redacted&&<RichContent text={turn.assistant_summary_redacted}/>}</article>)}
   {(session.tool_calls||[]).map(call=><details key={call.id}><summary>{call.tool_name} · tool evidence</summary><RichContent text={call.arguments_redacted||"No retained arguments"}/><RichContent text={call.result_preview_redacted||"No retained result"}/></details>)}
  </div>
 </details>
}
