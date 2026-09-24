/** @module UseCollaboration @description Loads and records the owner's review notes and decisions for the selected session. */
import {useCallback,useEffect,useState} from "react";
import {api} from "./api";
import type {Annotation,Review} from "./types";

export const ownerAuthor="local owner";
const refreshMs=15000;

export type Collaboration={
  annotations:Annotation[];
  reviews:Review[];
  error:string;
  addNote:(body:string,turnID?:string)=>Promise<void>;
  recordDecision:(status:string,summary:string)=>Promise<void>;
};

export function useCollaboration(sessionID:string|undefined):Collaboration{
  const[annotations,setAnnotations]=useState<Annotation[]>([]);
  const[reviews,setReviews]=useState<Review[]>([]);
  const[error,setError]=useState("");
  const refresh=useCallback(async()=>{
    if(!sessionID)return;
    const[notes,decisions]=await Promise.all([api.annotations(sessionID),api.reviews(sessionID)]);
    setAnnotations(notes);setReviews(decisions);setError("");
  },[sessionID]);
  useEffect(()=>{
    setAnnotations([]);setReviews([]);setError("");
    if(!sessionID)return;
    let active=true;
    const load=()=>Promise.all([api.annotations(sessionID),api.reviews(sessionID)])
      .then(([notes,decisions])=>{if(active){setAnnotations(notes);setReviews(decisions);setError("")}})
      .catch(value=>{if(active)setError(message(value))});
    load();
    // Supporters and invitees add notes from the bridge; pick them up without reopening the session.
    const timer=window.setInterval(load,refreshMs);
    return()=>{active=false;window.clearInterval(timer)};
  },[sessionID]);
  const addNote=useCallback(async(body:string,turnID?:string)=>{
    if(!sessionID)return;
    try{await api.addAnnotation({session_id:sessionID,turn_id:turnID,author:ownerAuthor,body});await refresh()}
    catch(value){setError(message(value));throw value}
  },[sessionID,refresh]);
  const recordDecision=useCallback(async(status:string,summary:string)=>{
    if(!sessionID)return;
    try{await api.addReview({session_id:sessionID,reviewer:ownerAuthor,status,summary});await refresh()}
    catch(value){setError(message(value));throw value}
  },[sessionID,refresh]);
  return {annotations,reviews,error,addNote,recordDecision};
}

function message(value:unknown){return value instanceof Error?value.message:String(value)}
