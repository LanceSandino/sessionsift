/** @module SessionContext @description Displays observed client and execution metadata without guessing unavailable device details. */
import React from "react";
import {absoluteTime} from "../format";
import type {Session} from "../types";

export function SessionContext({session}:{session:Session}){
  const values=[
    ["Started",absoluteTime(session.started_at)],
    ["Client / device",session.originator],
    ["Surface",session.surface],
    ["Work type",session.session_kind?.replaceAll("_"," ")],
    ["Run location",session.runtime_location?`${session.runtime_location} (derived)`:undefined],
    ["Thread source",session.thread_source],
    ["Approvals",session.approval_mode],
    ["Sandbox",session.sandbox_policy],
    ["Network",session.network_access],
  ].filter((entry):entry is [string,string]=>Boolean(entry[1]));
  return <section className="card sessionContext"><div className="sectionTitle"><h3>Session context</h3><span className="pill">observed</span></div><dl>{values.map(([label,value])=><div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>{session.title_source==="renamed"&&<p className="titleSource">Title renamed in Codex</p>}</section>;
}
