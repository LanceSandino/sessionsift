/** @module LayeredSiftMark @description Renders the production vector mark selected from branding proposal 01. */
import React from "react";

export function LayeredSiftMark({className=""}:{className?:string}){
  return <svg className={className} data-mark="layered-sift" viewBox="0 0 64 64" aria-hidden="true" focusable="false">
    <path className="siftLayer siftLayerTop" d="M7 9.5C5.7 7.7 7 5.2 9.2 5.2h45.6c2.2 0 3.5 2.5 2.2 4.3l-7 9.2c-.5.7-1.3 1.1-2.2 1.1H16.2c-.9 0-1.7-.4-2.2-1.1z"/>
    <path className="siftLayer siftLayerMiddle" d="M14.4 27.1c-1.4-1.8-.1-4.4 2.2-4.4h30.8c2.3 0 3.6 2.6 2.2 4.4l-6.9 9c-.5.7-1.3 1.1-2.2 1.1h-17c-.9 0-1.7-.4-2.2-1.1z"/>
    <path className="siftLayer siftLayerLower" d="M21.8 44.7c-1.4-1.8-.1-4.4 2.2-4.4h16c2.3 0 3.6 2.6 2.2 4.4l-7.9 10.1a2.9 2.9 0 0 1-4.6 0z"/>
  </svg>;
}
