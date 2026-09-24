/** @module StatusBar @description The quiet status line: local agent reachability, library location and last refresh. */
import React from "react";
import {clockTime} from "../format";

export function StatusBar({online,location,refreshedAt,onDiagnostics}:{online:boolean;location:string;refreshedAt?:string;onDiagnostics:()=>void}){
  return <footer className="statusbar">
    <div className="row"><span className={`dot ${online?"":"bad"}`} aria-hidden="true"/><span>{online?"Local agent connected":"Local agent unreachable"}</span><span aria-hidden="true">·</span><span>localhost only</span></div>
    <div className="status-right"><span>{location}</span>{refreshedAt&&<span>Refreshed {clockTime(refreshedAt)}</span>}<button type="button" onClick={onDiagnostics}>Diagnostics <span className="mono" aria-hidden="true">↗</span></button></div>
  </footer>;
}
