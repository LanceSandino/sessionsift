/** @module DiagnosticExport @description Exports an allowlisted count-only problem report, never arbitrary session fields. */
import React,{useState} from "react";
import type {Session} from "../types";
import {sourceCounts} from "../sources";
import {Icon} from "./Icon";

export function diagnosticReport(sessions:Session[]){
 return {schema:"sessionsift.support.v1",app_version:"0.1.0",counts:{sessions:sessions.length,...sourceCounts(sessions),parser_warnings:sessions.reduce((sum,s)=>sum+(s.warnings?.length||0),0)},privacy:"No session content, IDs, titles, paths, identities, tokens, URLs, or raw logs included."};
}
export function DiagnosticExport({sessions}:{sessions:Session[]}){
 const[preview,setPreview]=useState(false);
 const report=JSON.stringify(diagnosticReport(sessions),null,2);
 const download=()=>{const url=URL.createObjectURL(new Blob([report],{type:"application/json"}));const link=document.createElement("a");link.href=url;link.download="sessionsift-diagnostics.json";link.click();URL.revokeObjectURL(url)};
 return <section className="device-card diagnosticExport"><h3><Icon name="file"/>Report a problem</h3><p>Preview a count-only report you can send to the host. Nothing is uploaded automatically.</p>
  <div className="row"><button type="button" className="button" onClick={()=>setPreview(!preview)} aria-expanded={preview}>Preview safe report</button>{preview&&<button type="button" className="button primary" onClick={download}>Download JSON</button>}</div>
  {preview&&<pre className="report-preview">{report}</pre>}
 </section>
}
