/** @module Dialog @description Native modal dialogs: open-state syncing, backdrop dismissal and a titled dialog whose content mounts only while open. */
import React,{useEffect,useId,useRef,type RefObject} from "react";
import {Icon} from "./Icon";

/** Keeps a native dialog's modal state in step with React state, and reports native closes (Escape). */
export function useNativeModal(ref:RefObject<HTMLDialogElement|null>,open:boolean,onClose:()=>void){
  const close=useRef(onClose);
  close.current=onClose;
  useEffect(()=>{
    const dialog=ref.current;
    if(!dialog)return;
    // Test DOMs and older engines lack showModal; the open attribute is the fallback.
    if(open&&!dialog.open){if(typeof dialog.showModal==="function")dialog.showModal();else dialog.setAttribute("open","")}
    if(!open&&dialog.open){if(typeof dialog.close==="function")dialog.close();else dialog.removeAttribute("open")}
  },[ref,open]);
  useEffect(()=>{
    const dialog=ref.current;
    if(!dialog)return;
    const handle=()=>close.current();
    dialog.addEventListener("close",handle);
    return()=>dialog.removeEventListener("close",handle);
  },[ref]);
  return {
    onClick:(event:React.MouseEvent<HTMLDialogElement>)=>{
      if(event.target!==event.currentTarget)return;
      const box=event.currentTarget.getBoundingClientRect();
      if(event.clientX<box.left||event.clientX>box.right||event.clientY<box.top||event.clientY>box.bottom)close.current();
    },
    onKeyDown:(event:React.KeyboardEvent<HTMLDialogElement>)=>{if(event.key==="Escape"&&typeof ref.current?.showModal!=="function")close.current()},
  };
}

type Props={open:boolean;title:string;wide?:boolean;onClose:()=>void;children:React.ReactNode};

export function Dialog({open,title,wide,onClose,children}:Props){
  const ref=useRef<HTMLDialogElement>(null);
  const titleID=useId();
  const handlers=useNativeModal(ref,open,onClose);
  return <dialog ref={ref} className={wide?"wide":undefined} aria-labelledby={titleID} {...handlers}>
    {open&&<>
      <div className="dialog-head"><h2 id={titleID}>{title}</h2><button type="button" className="icon-btn" aria-label="Close dialog" onClick={onClose}><Icon name="x"/></button></div>
      <div className="dialog-body">{children}</div>
    </>}
  </dialog>;
}
