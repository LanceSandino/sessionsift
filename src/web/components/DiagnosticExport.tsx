/** @module DiagnosticExport @description Exports an allowlisted count-only problem report, never arbitrary session fields. */
import React,{useState} from "react";
import type {Session} from "../types";

export function diagnosticReport(sessions:Session[]){
 return {schema:"sessionsift.support.v1",app_version:"0.1.0",counts:{sessions:sessions.length,codex:sessions.filter(s=>(s.source||"codex")==="codex").length,chatgpt:sessions.filter(s=>s.source==="chatgpt").length,parser_warnings:sessions.reduce((sum,s)=>sum+(s.warnings?.length||0),0)},privacy:"No session content, IDs, titles, paths, identities, tokens, URLs, or raw logs included."};
}
export function DiagnosticExport({sessions}:{sessions:Session[]}){
 const[preview,setPreview]=useState(false);
 const report=JSON.stringify(diagnosticReport(sessions),null,2);
 const download=()=>{const url=URL.createObjectURL(new Blob([report],{type:"application/json"}));const link=document.createElement("a");link.href=url;link.download="sessionsift-diagnostics.json";link.click();URL.revokeObjectURL(url)};
 return <section className="card diagnosticExport"><h3>Report a problem</h3><p>Preview a count-only report you can send to the host. Nothing is uploaded automatically.</p><button onClick={()=>setPreview(!preview)} aria-expanded={preview}>Preview safe report</button>{preview&&<><pre>{report}</pre><button onClick={download}>Download JSON</button></>}</section>
}
