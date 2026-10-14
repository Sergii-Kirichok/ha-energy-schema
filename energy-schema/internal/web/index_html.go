package web

// indexHTML auto-reloads the given SVG file every refresh seconds.
// %s = svg filename, %d = refresh seconds.
const indexHTML = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body{margin:0;height:100%%;overflow:hidden;background:#0f1115}#c{position:fixed;inset:0}#c svg{width:100%%;height:100%%;display:block}#vo{position:fixed;top:8px;right:12px;background:#1f2937;color:#9ca3af;font:12px sans-serif;padding:4px 10px;border-radius:8px;opacity:.85;z-index:9}
.face{transform-box:fill-box;transform-origin:center;transition:transform .22s ease-in-out,visibility 0s .22s}
.f-back{transform:scaleX(0);visibility:hidden;pointer-events:none}
.flipped>.f-front{transform:scaleX(0);visibility:hidden;pointer-events:none}
.flipped>.f-back{transform:scaleX(1);visibility:visible;pointer-events:auto;transition-delay:0s}
:not(.flipped)>.f-front{transition-delay:0s}
.noanim .face{transition:none}
#lost{position:fixed;top:8px;left:12px;background:#7f1d1d;color:#fecaca;font:13px sans-serif;padding:4px 10px;border-radius:8px;z-index:9;display:none}</style></head><body><div id="c"></div><div id="vo" style="display:none">только просмотр</div><div id="lost">нет связи · переподключение…</div><script>
var CANCTL=%s;
function ask(act,val){
 var m='Выполнить действие?';
 if(act==='avr_src'){m='Переключить питание Дома на: '+(val==='reserve'?'Резерв (стабилизаторы)':'Инвертор')+'?';}
 if(act==='avr3_src'){m='Питать Дом через АВР ген. от: '+(val==='gen'?'ГЕНЕРАТОРА (напрямую)':'АВР дома (инвертор/резерв)')+'?';}
 if(act==='contactor'){m='Переключить АВР вводов на: '+(val==='in2'?'Ввод 2 (Зелёный)':'Ввод 1 (Рыбхоз)')+'?';}
 if(act==='gen_start'){m='Запустить генератор?';}
 if(act==='gen_stop'){m='Остановить генератор?';}
 if(act==='gen_heater'){m=(val==='on'?'Включить':'Выключить')+' подогрев генератора?';}
 if(!confirm(m))return;
 fetch('control?act='+act+'&val='+val,{method:'POST'}).then(function(r){
  if(!r.ok){r.text().then(function(t){alert('Не выполнено: '+t);});}else{setTimeout(load,400);}
 }).catch(function(e){alert('Ошибка связи: '+e);});
}
// оборот карточки АКБ: правка без немедленной записи — изменённое в QP
// (оранжевая рамка), «Сохранить» шлёт всё разом, после перечитывания схемы
// QS сверяет: совпало — зелёная рамка, нет — красная
var QP={},QS={};
function qq(s){return document.querySelectorAll('#c '+s)}
function qcur(k){var e=document.querySelector('#c [data-qn="'+k+'"],#c [data-qb="'+k+'"]');return e?+e.getAttribute('data-v'):NaN}
function qpaint(){
 var i,e,k;
 var ns=qq('[data-qn]');for(i=0;i<ns.length;i++){e=ns[i];k=e.getAttribute('data-qn');if(k in QP)e.textContent=QP[k]+' '+e.getAttribute('data-u')}
 var bs=qq('[data-qb]');for(i=0;i<bs.length;i++){e=bs[i];k=e.getAttribute('data-qb');if(!(k in QP))continue;var on=QP[k]==1,c=on?'#22c55e':'#6b7280',r=e.querySelector('rect'),t=e.querySelector('text');r.setAttribute('fill',c);r.setAttribute('stroke',c);t.setAttribute('fill',c);t.textContent=on?'ВКЛ':'ВЫКЛ'}
 var rs=qq('[data-qrow]');for(i=0;i<rs.length;i++){e=rs[i];k=e.getAttribute('data-qrow');e.setAttribute('stroke',(k in QP)?'#f59e0b':(QS[k]&&QS[k].d)?(QS[k].ok?'#22c55e':'#ef4444'):'transparent')}
 var sv=document.querySelector('#c [data-qsave]');if(sv){var n=Object.keys(QP).length,c2=n?'#f59e0b':'#2a2f38';sv.querySelector('rect').setAttribute('stroke',c2);sv.querySelector('rect').setAttribute('fill',c2);sv.querySelector('text').setAttribute('fill',n?'#f59e0b':'#9ca3af');sv.querySelector('text').textContent=n?'Сохранить · '+n:'Сохранить'}
}
function qverify(){var now=Date.now();for(var k in QS){var s=QS[k];if(!s.d){if(qcur(k)==s.v){s.d=1;s.ok=1;s.t=now}else if(now-s.t>15000){s.d=1;s.ok=0;s.t=now}}else if(now-s.t>6000)delete QS[k]}}
function qwire(){
 var ds=qq('[data-qd]');for(var i=0;i<ds.length;i++)(function(el){el.addEventListener('click',function(){var k=el.getAttribute('data-qd'),n=document.querySelector('#c [data-qn="'+k+'"]');if(!n)return;
  var b=(k in QP)?QP[k]:+n.getAttribute('data-v'),v=Math.min(+n.getAttribute('data-max'),Math.max(+n.getAttribute('data-min'),b+(+el.getAttribute('data-d'))));
  if(v==+n.getAttribute('data-v'))delete QP[k];else QP[k]=v;qpaint()})})(ds[i]);
 var bs=qq('[data-qb]');for(var j=0;j<bs.length;j++)(function(el){el.addEventListener('click',function(){var k=el.getAttribute('data-qb'),cur=+el.getAttribute('data-v'),v=1-((k in QP)?QP[k]:cur);
  if(v==cur)delete QP[k];else QP[k]=v;qpaint()})})(bs[j]);
 var sv=document.querySelector('#c [data-qsave]');if(sv)sv.addEventListener('click',function(){var ks=Object.keys(QP);if(!ks.length)return;
  var val=ks.map(function(k){return k+'='+QP[k]}).join(',');
  fetch('control?act=qsave&val='+encodeURIComponent(val),{method:'POST'}).then(function(r){if(!r.ok){r.text().then(function(t){alert('Не сохранено: '+t)});return}
   var now=Date.now();ks.forEach(function(k){QS[k]={v:QP[k],t:now,d:0}});QP={};qpaint();setTimeout(load,1500)}).catch(function(e){alert('Ошибка связи: '+e)})});
}
function flips(){var g=document.getElementById('card-batt');if(!g)return;
 if(localStorage.getItem('flip_batt')==='1'){g.classList.add('noanim','flipped');setTimeout(function(){g.classList.remove('noanim');},60);}
 var els=document.querySelectorAll('#c [data-flip]');for(var i=0;i<els.length;i++){els[i].addEventListener('click',function(){var on=!g.classList.contains('flipped');g.classList.toggle('flipped',on);localStorage.setItem('flip_batt',on?'1':'0');});}}
function $$nav(){var n=document.querySelectorAll('#c [data-nav]');for(var i=0;i<n.length;i++){(function(el){el.addEventListener('click',function(){location.href=el.getAttribute('data-nav')})})(n[i])}}
function wire(){flips();$$nav();if(!CANCTL)return;var els=document.querySelectorAll('#c [data-act]');for(var i=0;i<els.length;i++){(function(el){el.addEventListener('click',function(){ask(el.getAttribute('data-act'),el.getAttribute('data-val'));});})(els[i]);}
 qwire()}
var T0=Date.now(),busy=0,okAt=Date.now();
// Устойчиво для ТВ-браузеров: таймаут запроса, без наложения запросов, ошибка
// не стирает схему (плашка «нет связи»), долго без связи — перезагрузка страницы.
function lost(on){document.getElementById('lost').style.display=on?'block':'none'}
function load(){if(busy&&Date.now()-busy<20000)return;busy=Date.now();
 var ac=window.AbortController?new AbortController():null,to=setTimeout(function(){if(ac)ac.abort()},15000);
 fetch('%s?t='+Date.now(),ac?{signal:ac.signal,cache:'no-store'}:{cache:'no-store'}).then(function(r){if(!r.ok)throw new Error('HTTP '+r.status);return r.text()}).then(function(t){
  if(t.indexOf('<svg')<0)throw new Error('not svg');var c=document.getElementById('c');c.innerHTML=t;var s=c.querySelector('svg');if(s&&s.setCurrentTime){try{s.setCurrentTime((Date.now()-T0)/1000);}catch(e){}}wire();qverify();qpaint();okAt=Date.now();lost(false)
 }).catch(function(){lost(true)}).then(function(){clearTimeout(to);busy=0})}
if(!CANCTL){document.getElementById('vo').style.display='block';}
var EVERY=%d000;load();setInterval(function(){load();if(Date.now()-okAt>Math.max(90000,3*EVERY))location.reload()},EVERY);
setTimeout(function(){location.reload()},6*3600*1000); // ТВ-браузеры копят память при постоянной замене SVG</script></body></html>`
