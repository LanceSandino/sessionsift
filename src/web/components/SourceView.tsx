/** @module SourceView @description Shows the session's normalized, redacted record as bounded JSON text; it is not the original import file. */
import React,{useMemo} from "react";
import type {Session} from "../types";

const displayLimit=200000;

export function SourceView({session}:{session:Session}){
  const text=useMemo(()=>JSON.stringify(session,null,2),[session]);
  const download=()=>{
    const url=URL.createObjectURL(new Blob([text],{type:"application/json"}));
    const link=document.createElement("a");link.href=url;link.download="sessionsift-session.json";link.click();URL.revokeObjectURL(url);
  };
  return <section className="source-view" aria-label="Normalized session record">
    <header><span>Normalized record · redacted at import · not the original transcript file</span><button type="button" className="button" onClick={download}>Download JSON</button></header>
    <pre>{text.length>displayLimit?`${text.slice(0,displayLimit)}\n\n… ${(text.length-displayLimit).toLocaleString()} more characters. Download the JSON to read the rest.`:text}</pre>
  </section>;
}
