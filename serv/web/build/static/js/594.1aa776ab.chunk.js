"use strict";(self.webpackChunkweb_ui=self.webpackChunkweb_ui||[]).push([[594],{6594:function(e,r,n){n.r(r),n.d(r,{b:function(){return a}});var t=n(8308),i=Object.defineProperty,o=function(e,r){return i(e,"name",{value:r,configurable:!0})};function f(e,r){for(var n=function(){var n=r[t];if("string"!=typeof n&&!Array.isArray(n)){var i=function(r){if("default"!==r&&!(r in e)){var t=Object.getOwnPropertyDescriptor(n,r);t&&Object.defineProperty(e,r,t.get?t:{enumerable:!0,get:function(){return n[r]}})}};for(var o in n)i(o)}},t=0;t<r.length;t++)n();return Object.freeze(Object.defineProperty(e,Symbol.toStringTag,{value:"Module"}))}o(f,"_mergeNamespaces");!function(e){function r(r){return function(n,t){var i=t.line,f=n.getLine(i);function l(r){for(var o,l=t.ch,a=0;;){var u=l<=0?-1:f.lastIndexOf(r[0],l-1);if(-1!=u){if(1==a&&u<t.ch)break;if(o=n.getTokenTypeAt(e.Pos(i,u+1)),!/^(comment|string)/.test(o))return{ch:u+1,tokenType:o,pair:r};l=u-1}else{if(1==a)break;a=1,l=f.length}}}function a(r){var t,o,f=1,l=n.lastLine(),a=r.ch;e:for(var u=i;u<=l;++u)for(var s=n.getLine(u),c=u==i?a:0;;){var g=s.indexOf(r.pair[0],c),p=s.indexOf(r.pair[1],c);if(g<0&&(g=s.length),p<0&&(p=s.length),(c=Math.min(g,p))==s.length)break;if(n.getTokenTypeAt(e.Pos(u,c+1))==r.tokenType)if(c==g)++f;else if(!--f){t=u,o=c;break e}++c}return null==t||i==t?null:{from:e.Pos(i,a),to:e.Pos(t,o)}}o(l,"findOpening"),o(a,"findRange");for(var u=[],s=0;s<r.length;s++){var c=l(r[s]);c&&u.push(c)}for(u.sort((function(e,r){return e.ch-r.ch})),s=0;s<u.length;s++){var g=a(u[s]);if(g)return g}return null}}o(r,"bracketFolding"),e.registerHelper("fold","brace",r([["{","}"],["[","]"]])),e.registerHelper("fold","brace-paren",r([["{","}"],["[","]"],["(",")"]])),e.registerHelper("fold","import",(function(r,n){function t(n){if(n<r.firstLine()||n>r.lastLine())return null;var t=r.getTokenAt(e.Pos(n,1));if(/\S/.test(t.string)||(t=r.getTokenAt(e.Pos(n,t.end+1))),"keyword"!=t.type||"import"!=t.string)return null;for(var i=n,o=Math.min(r.lastLine(),n+10);i<=o;++i){var f=r.getLine(i).indexOf(";");if(-1!=f)return{startCh:t.end,end:e.Pos(i,f)}}}o(t,"hasImport");var i,f=n.line,l=t(f);if(!l||t(f-1)||(i=t(f-2))&&i.end.line==f-1)return null;for(var a=l.end;;){var u=t(a.line+1);if(null==u)break;a=u.end}return{from:r.clipPos(e.Pos(f,l.startCh+1)),to:a}})),e.registerHelper("fold","include",(function(r,n){function t(n){if(n<r.firstLine()||n>r.lastLine())return null;var t=r.getTokenAt(e.Pos(n,1));return/\S/.test(t.string)||(t=r.getTokenAt(e.Pos(n,t.end+1))),"meta"==t.type&&"#include"==t.string.slice(0,8)?t.start+8:void 0}o(t,"hasInclude");var i=n.line,f=t(i);if(null==f||null!=t(i-1))return null;for(var l=i;null!=t(l+1);)++l;return{from:e.Pos(i,f+1),to:r.clipPos(e.Pos(l))}}))}((0,t.r)());var l={},a=f({__proto__:null,default:(0,t.g)(l)},[l])}}]);
//# sourceMappingURL=594.1aa776ab.chunk.js.map