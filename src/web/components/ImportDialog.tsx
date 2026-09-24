/** @module ImportDialog @description Explains how each source reaches the local library; coding sessions are watched, chat exports are imported explicitly. */
import React from "react";
import {Dialog} from "./Dialog";

const intake=[
  {badge:"CC",tone:"cc",title:"Claude Code",how:"Watched automatically",detail:"Transcripts under ~/.claude/projects (or CLAUDE_CONFIG_DIR) appear as they change.",command:"sessionsift import"},
  {badge:"CX",tone:"cx",title:"Codex",how:"Watched automatically",detail:"Sessions under ~/.codex/sessions and archived_sessions (or CODEX_HOME) appear as they change.",command:"sessionsift import"},
  {badge:"Cl",tone:"cl",title:"Claude.ai",how:"Import an export",detail:"Request a data export from Claude, then import the downloaded file.",command:"sessionsift import-claude <export.zip>"},
  {badge:"G",tone:"gpt",title:"ChatGPT",how:"Import an export",detail:"Request an export in Settings → Data Controls, then import the downloaded file.",command:"sessionsift import-chatgpt <export.zip>"},
];

export function ImportDialog({open,onClose}:{open:boolean;onClose:()=>void}){
  return <Dialog open={open} title="Bring your conversations together" onClose={onClose}>
    <p>One local library for coding sessions and exported chats. SessionSift reads only these sources and never scrapes accounts or browser state.</p>
    <div className="source-list">{intake.map(source=><div className="dialog-session" key={source.title}><span className={`source-icon ${source.tone}`} aria-hidden="true">{source.badge}</span><div className="grow"><strong>{source.title}</strong><small>{source.detail}</small><code className="command-line">{source.command}</code></div><span className="badge">{source.how}</span></div>)}</div>
    <div className="privacy-line"><span>Run commands on this machine. Re-running <code>sessionsift import</code> processes only changed transcript files; every import is redacted before it is stored.</span></div>
    <div className="dialog-footer"><button type="button" className="button" onClick={onClose}>Back to the workspace</button></div>
  </Dialog>;
}
