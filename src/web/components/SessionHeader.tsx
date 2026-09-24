/** @module SessionHeader @description The contextual session title, its observed metadata and the current review state. */
import React from "react";
import {duration,shortDateTime} from "../format";
import type {Decision} from "../reviewState";
import {sourceInfo} from "../sources";
import type {Session} from "../types";
import {Icon} from "./Icon";

export function SessionHeader({session,decision}:{session:Session;decision:Decision}){
  const info=sourceInfo(session);
  const metrics=session.metrics;
  const model=session.model||session.turns?.find(turn=>turn.model)?.model;
  return <div className="session-header">
    <div className="grow">
      <h1>{session.title}</h1>
      <div className="metadata">
        <span className={`source-icon ${info.tone}`} aria-hidden="true">{info.badge}</span><span>{info.label}</span>
        {model&&<><span aria-hidden="true">·</span><span>{model}</span></>}
        <span className="desktop-meta" aria-hidden="true">·</span><span className="desktop-meta">{shortDateTime(session.started_at)}</span>
        {metrics&&<><span aria-hidden="true">·</span><span>{duration(metrics.duration_ms)} active / {duration(metrics.elapsed_duration_ms)} elapsed</span></>}
        {session.warnings?.length?<><span aria-hidden="true">·</span><span className="warn-meta" title={session.warnings.join("\n")}>{session.warnings.length} parser warning{session.warnings.length===1?"":"s"}</span></>:null}
      </div>
    </div>
    <div className={`status-pill ${decision.kind==="followup"?"followup":decision.kind==="pending"?"pending":""}`} title="Current review decision"><Icon name={decision.kind==="followup"?"note":"circle-check"}/><span className="status-text">{decision.label}</span></div>
  </div>;
}
