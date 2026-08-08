const $ = (s) => document.querySelector(s);
const $$ = (s) => [...document.querySelectorAll(s)];
const money = new Intl.NumberFormat('zh-TW',{style:'currency',currency:'TWD',maximumFractionDigits:0});
const number = new Intl.NumberFormat('zh-TW',{maximumFractionDigits:4});
let transactions = [], portfolio = {holdings:[],summary:{}};

async function api(url, options={}) {
  const response = await fetch(url,{headers:{'Content-Type':'application/json'},...options});
  if (!response.ok) { const body=await response.json().catch(()=>({})); throw new Error(body.error||'操作失敗'); }
  return response.status===204 ? null : response.json();
}

async function load() {
  try {
    const params = new URLSearchParams();
    if ($('#filterSymbol').value) params.set('symbol',$('#filterSymbol').value);
    if ($('#filterFrom').value) params.set('from',$('#filterFrom').value);
    if ($('#filterTo').value) params.set('to',$('#filterTo').value);
    [portfolio,transactions] = await Promise.all([api('/api/portfolio'),api('/api/transactions?'+params)]);
    render();
  } catch(err) { toast(err.message); }
}

function render(){ renderSummary(); renderHoldings(); renderTransactions(); renderReview(); renderSymbolFilter(); }
function pnlClass(v){return v>0?'positive':v<0?'negative':'neutral'}
function renderSummary(){
  const s=portfolio.summary;
  $('#totalValue').textContent=money.format(s.totalValue||0); $('#totalCost').textContent=money.format(s.totalCost||0); $('#realized').textContent=money.format(s.realized||0);
  const p=s.unrealizedPercent||0; $('#totalPnl').className='change '+pnlClass(p); $('#totalPnl').textContent=(p>=0?'＋':'')+p.toFixed(2)+'%　'+money.format(s.unrealized||0);
}
function renderHoldings(){
  $('#holdingCount').textContent=`${portfolio.holdings.length} 個項目`;
  $('#holdings').innerHTML=portfolio.holdings.length?portfolio.holdings.map(h=>`<article class="holding-card"><div class="holding-top"><div class="asset-id"><span class="asset-avatar">${escape(h.symbol).slice(0,3)}</span><span class="asset-name"><strong>${escape(h.name)}</strong><small>${escape(h.symbol)}</small></span></div><button class="price-edit" data-price="${escape(h.symbol)}" data-current="${h.currentPrice}" title="更新目前價格">✎</button></div><div class="holding-value"><strong>${money.format(h.marketValue)}</strong><span class="${pnlClass(h.unrealized)}">${h.unrealized>=0?'＋':''}${h.unrealizedPercent.toFixed(2)}%</span></div><div class="allocation-bar"><span style="width:${Math.min(h.allocation,100)}%"></span></div><div class="holding-footer"><span>${number.format(h.quantity)} 股 · 均價 ${money.format(h.averageCost)}</span><span>${h.allocation.toFixed(1)}%</span></div></article>`).join(''):`<div class="empty"><strong>從第一筆交易開始</strong>新增買入紀錄後，持股與總值會自動出現在這裡。</div>`;
}
function renderTransactions(){
  $('#transactionRows').innerHTML=transactions.length?transactions.slice().reverse().map(t=>`<tr><td class="mono">${t.tradedAt}</td><td><strong>${escape(t.symbol)}</strong><br><small>${escape(t.name)}</small></td><td><span class="pill ${t.type}">${t.type==='buy'?'買入':'賣出'}</span></td><td class="mono">${number.format(t.quantity)}</td><td class="mono">${money.format(t.price)}</td><td class="mono">${money.format(t.quantity*t.price+t.fee)}</td><td><div class="row-actions"><button data-edit="${t.id}" aria-label="編輯">✎</button><button data-delete="${t.id}" aria-label="刪除">×</button></div></td></tr>`).join(''):`<tr><td colspan="7"><div class="empty"><strong>沒有符合條件的交易</strong>調整篩選條件，或新增一筆交易。</div></td></tr>`;
}
function renderReview(){
  $('#reviewList').innerHTML=transactions.length?transactions.slice().reverse().map(t=>`<article class="review-item"><div class="review-date">${t.tradedAt}</div><div class="review-content"><strong>${escape(t.name)} <small>${escape(t.symbol)}</small> · ${t.type==='buy'?'買入':'賣出'}</strong><p>${escape(t.note)||'這筆交易沒有留下備註。'}</p></div><div class="review-number">${number.format(t.quantity)} × ${money.format(t.price)}</div></article>`).join(''):`<div class="empty"><strong>還沒有決策可以回顧</strong>在每筆交易留下當時的想法，未來會很有價值。</div>`;
}
function renderSymbolFilter(){ const current=$('#filterSymbol').value; const assets=[...new Map([...portfolio.holdings.map(h=>[h.symbol,h.name]),...transactions.map(t=>[t.symbol,t.name])]).entries()]; $('#filterSymbol').innerHTML='<option value="">全部標的</option>'+assets.map(([s,n])=>`<option value="${escape(s)}">${escape(s)} · ${escape(n)}</option>`).join(''); $('#filterSymbol').value=current; }

function openTrade(t){ $('#tradeForm').reset(); $('#fee').value=0; $('#tradedAt').value=new Date().toLocaleDateString('en-CA'); $('#tradeId').value=''; $('#dialogTitle').textContent='新增交易'; if(t){$('#dialogTitle').textContent='編輯交易';$('#tradeId').value=t.id;$('#symbol').value=t.symbol;$('#name').value=t.name;$('#quantity').value=t.quantity;$('#price').value=t.price;$('#fee').value=t.fee;$('#tradedAt').value=t.tradedAt;$('#note').value=t.note;document.querySelector(`input[name=type][value=${t.type}]`).checked=true} $('#tradeDialog').showModal(); }
function closeTrade(){ $('#tradeDialog').close(); }
function toast(message){const el=$('#toast');el.textContent=message;el.classList.add('show');setTimeout(()=>el.classList.remove('show'),2400)}
function escape(value){const d=document.createElement('div');d.textContent=String(value??'');return d.innerHTML}

$$('.nav-item').forEach(b=>b.addEventListener('click',()=>{$$('.nav-item').forEach(x=>x.classList.remove('active'));b.classList.add('active');$$('.view').forEach(v=>v.classList.remove('active-view'));$('#'+b.dataset.view).classList.add('active-view');const titles={overview:['PORTFOLIO OVERVIEW','投資總覽'],transactions:['TRANSACTION LEDGER','交易紀錄'],review:['DECISION REVIEW','投資 Review']};$('#eyebrow').textContent=titles[b.dataset.view][0];$('#pageTitle').textContent=titles[b.dataset.view][1]}));
$('#addTrade').addEventListener('click',()=>openTrade()); $('#closeDialog').addEventListener('click',closeTrade); $('#cancelDialog').addEventListener('click',closeTrade);
$('#tradeForm').addEventListener('submit',async e=>{e.preventDefault();const id=$('#tradeId').value;const body={symbol:$('#symbol').value,name:$('#name').value,type:$('input[name=type]:checked').value,quantity:Number($('#quantity').value),price:Number($('#price').value),fee:Number($('#fee').value||0),tradedAt:$('#tradedAt').value,note:$('#note').value};try{await api(id?'/api/transactions/'+id:'/api/transactions',{method:id?'PUT':'POST',body:JSON.stringify(body)});closeTrade();toast(id?'交易已更新':'交易已新增');await load()}catch(err){toast(err.message)}});
document.addEventListener('click',async e=>{const edit=e.target.closest('[data-edit]'),del=e.target.closest('[data-delete]'),price=e.target.closest('[data-price]');if(edit)openTrade(transactions.find(t=>t.id===Number(edit.dataset.edit)));if(del&&confirm('確定要刪除這筆交易嗎？')){try{await api('/api/transactions/'+del.dataset.delete,{method:'DELETE'});toast('交易已刪除');await load()}catch(err){toast(err.message)}}if(price){const value=prompt(`更新 ${price.dataset.price} 的目前價格`,price.dataset.current);if(value!==null&&!Number.isNaN(Number(value))){try{await api(`/api/assets/${price.dataset.price}/price`,{method:'PUT',body:JSON.stringify({price:Number(value)})});toast('目前價格已更新');await load()}catch(err){toast(err.message)}}}});
['filterSymbol','filterFrom','filterTo'].forEach(id=>$('#'+id).addEventListener('change',load));$('#clearFilters').addEventListener('click',()=>{$('#filterSymbol').value='';$('#filterFrom').value='';$('#filterTo').value='';load()});
load();
