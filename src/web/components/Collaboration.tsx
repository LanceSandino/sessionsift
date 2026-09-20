/** @module Collaboration @description Shows redacted annotations and review outcomes for a session. */
import React,{useCallback,useEffect,useState} from "react";
import {api} from "../api";
import type {Annotation,Review} from "../types";

export function Collaboration({sessionId}:{sessionId:string}){
  const[annotations,setAnnotations]=useState<Annotation[]>([]);const[reviews,setReviews]=useState<Review[]>([]);const[body,setBody]=useState("");const[summary,setSummary]=useState("");const[error,setError]=useState("");
  const refresh=useCallback(()=>Promise.all([api.annotations(sessionId),api.reviews(sessionId)]).then(([a,r])=>{setAnnotations(a);setReviews(r);setError("")}).catch(value=>setError(String(value))),[sessionId]);
  useEffect(()=>{refresh()},[refresh]);
  const annotate=()=>api.addAnnotation({session_id:sessionId,author:"local owner",body}).then(()=>{setBody("");return refresh()}).catch(value=>setError(String(value)));
  const review=()=>api.addReview({session_id:sessionId,reviewer:"local owner",status:"approved",summary}).then(()=>{setSummary("");return refresh()}).catch(value=>setError(String(value)));
  return <section className="card collaboration"><div className="sectionTitle"><h3>Review & annotations</h3><span className="pill">{annotations.length+reviews.length}</span></div>{error&&<small className="dangerText">{error}</small>}{annotations.map(value=><div className="collaborationItem" key={value.id}><b>{value.author}</b><small>{new Date(value.created_at).toLocaleString()}</small><p>{value.body}</p></div>)}{reviews.map(value=><div className="collaborationItem" key={value.id}><b>{value.reviewer} · {value.status}</b><small>{new Date(value.created_at).toLocaleString()}</small><p>{value.summary}</p></div>)}<div className="collaborationForm"><input aria-label="New annotation" value={body} onChange={event=>setBody(event.target.value)} placeholder="Add an annotation"/><button disabled={!body} onClick={annotate}>Add</button><input aria-label="Review summary" value={summary} onChange={event=>setSummary(event.target.value)} placeholder="Record an approval summary"/><button disabled={!summary} onClick={review}>Approve</button></div></section>
}
