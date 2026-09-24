/** @module NoteList @description Review notes as cards with author, target and time; the transcript itself is never changed. */
import React from "react";
import {shortDateTime} from "../format";
import {newestFirst,noteTarget} from "../reviewState";
import type {Annotation,Turn} from "../types";
import {Icon} from "./Icon";

export function NoteList({annotations,turns}:{annotations:Annotation[];turns:Turn[]}){
  if(!annotations.length)return <div className="empty"><Icon name="note"/><br/>No review notes yet.<br/><span className="small">Add an annotation without changing the transcript.</span></div>;
  return <div className="note-list">{newestFirst(annotations).map(note=><article className="note-card" key={note.id}>
    <div className="row"><span className="avatar" aria-hidden="true">{note.author.slice(0,1).toUpperCase()}</span><strong>{note.author}</strong><span className="muted">· {noteTarget(note,turns)}</span><small className="muted note-time">{shortDateTime(note.created_at)}</small></div>
    <p>{note.body}</p>
  </article>)}</div>;
}
