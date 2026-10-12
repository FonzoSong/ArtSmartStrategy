// @ts-nocheck
import "./styles.css";
import { requestJSON, resolveAPIURL } from "./api/client";
import { WORKFLOW, WORKFLOW_COLORS, WORKFLOW_STATES } from "./components/workflow";
import { pageLabels as pages } from "./pages/routes";
import { formatCount, formatDecimal, formatPercent, formatPercentagePoints, formatScore, toFiniteNumber } from "./lib/format";
import { dateTimeText, localDateKey, monthAnchor, monthGridDates, monthRange, pureDateKey, weekdayLabelOf, withinWindow } from "./lib/dates";
import { normalizeAPIResponse } from "./lib/contracts";
import { NO_FINISHED_VIDEO_TEXT, isReadySuggestionCandidate, showNoFinishedVideoEmptyState } from "./lib/suggestions";

const CATEGORIES=['演出宣传','技艺展示','幕后记录','艺术科普'];
const WEEKDAYS=['周一','周二','周三','周四','周五','周六','周日'];
const initialPage=pages[location.hash.slice(1)]?location.hash.slice(1):'overview';
const state={page:initialPage,teamId:Number(localStorage.getItem('artsTeamId')||1),user:null,calendarMode:'month',monthOffset:0,records:[],recordsSearch:'',recordsStatus:'',recordsLifecycle:'',recordsTag:'',recordsPlatform:'',recordsCategory:'',recordsOwner:'',recordsSort:'published_at',recordsOrder:'desc',recordsPage:1,recordsPageSize:10,selectedRecords:[],overview:null,settings:null,recommendation:null,draft:null,calendar:[],hypotheses:[],performances:[],users:[],runtimeMode:null,tags:[],rules:[],schedulePolicy:null,scheduleWindow:null,scheduleWindowRows:[],systemRules:[],ruleLogs:[],overviewRange:'30d',overviewPlatform:'',overviewCategory:''};
const qs=(s,root=document)=>root.querySelector(s),qsa=(s,root=document)=>[...root.querySelectorAll(s)];
const pad=n=>String(n).padStart(2,'0');
const escapeHtml=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const fmt=formatCount;
const num=(value,fallback=0)=>toFiniteNumber(value)??fallback;
const asArray=value=>Array.isArray(value)?value:[];
function toast(msg){const el=qs('#toast');el.textContent=msg;el.classList.add('show');clearTimeout(window.toastTimer);window.toastTimer=setTimeout(()=>el.classList.remove('show'),3200)}
function apiHeaders(extra={}){const token=sessionStorage.getItem('artsSessionToken');return {'X-Team-Id':String(state.teamId),...(token?{'Authorization':`Bearer ${token}`}:{ }),...extra}}
async function api(url,options={}){const headers=apiHeaders(options.headers||{});if(!(options.body instanceof FormData)&&options.body)headers['Content-Type']='application/json';return normalizeAPIResponse(url,await requestJSON(url,options,headers))}
async function downloadApi(path){try{const response=await fetch(resolveAPIURL(path),{headers:apiHeaders()});if(!response.ok){let message=`导出失败（${response.status}）`;try{const body=await response.json();message=body.detail||message}catch{}throw new Error(message)}const disposition=response.headers.get('content-disposition')||'';const encoded=disposition.match(/filename\*=UTF-8''([^;]+)/i);const plain=disposition.match(/filename=\"?([^;\"]+)/i);const filename=encoded?decodeURIComponent(encoded[1]):plain?plain[1]:'export.bin';const url=URL.createObjectURL(await response.blob());const a=document.createElement('a');a.href=url;a.download=filename;document.body.appendChild(a);a.click();a.remove();setTimeout(()=>URL.revokeObjectURL(url),1000)}catch(e){toast(e.message)}}
function heading(kicker,title,desc,actions=''){return `<div class="page-heading"><div><div class="eyebrow">${kicker}</div><h1>${title}</h1><p>${desc}</p></div><div class="heading-actions">${actions}</div></div>`}
function emptyState(title='从一条记录开始，留住团队的好经验'){return `<div class="empty-state"><div class="empty-icon">❋</div><h3>${title}</h3><p>录入团队自己的发布记录后，这里会呈现真实的传播表现和复盘线索。</p><button class="btn-primary" data-goto="records">＋ 录入内容</button></div>`}
function statCard(label,value,foot,icon='◫'){return `<div class="stat-card"><div class="stat-label"><span class="stat-icon">${icon}</span>${label}</div><div class="stat-value">${value}</div><div class="stat-foot">${foot}</div></div>`}
function sampleLabel(n,min){return n<min?'样本不足':n<=min*2?'样本较少，仅供参考':`${n} 条记录`}
function tag(text,cls='type-tag'){return `<span class="${cls}">${escapeHtml(text||'—')}</span>`}
function scoreText(value){return value==null?'样本不足':`${Math.round(value)}分`}
function refresh(){return api('/api/contents').then(d=>state.records=d.items).then(()=>api('/api/performances')).then(d=>state.performances=d).then(()=>api('/api/users')).then(d=>state.users=d).then(()=>api('/api/tags')).then(d=>state.tags=d).then(()=>api('/api/stats/overview')).then(d=>state.overview=d).then(()=>api('/api/settings')).then(d=>state.settings=d)}
function syncPageChrome(){qsa('.nav-item[data-page]').forEach(b=>b.classList.toggle('active',b.dataset.page===state.page));qs('#crumb').textContent=pages[state.page]||pages.overview}
async function go(page){page=Object.hasOwn(pages,page)?page:'overview';state.page=page;if(location.hash!==`#${page}`)history.pushState(null,'',`#${page}`);syncPageChrome();qs('#sidebar').classList.remove('open');await render()}
function routeFromLocation(){if(!state.user)return;const page=location.hash.slice(1);if(!Object.hasOwn(pages,page)||page===state.page)return;state.page=page;syncPageChrome();render()}
window.addEventListener('popstate',routeFromLocation);window.addEventListener('hashchange',routeFromLocation)
async function render(){const root=qs('#pageRoot');root.innerHTML='<section class="panel loading-state"><span class="loading-spinner"></span>正在加载…</section>';try{await refresh();const fn={overview:overviewPage,records:recordsPage,analysis:analysisPage,suggestions:suggestionsPage,calendar:calendarPage,settings:settingsPage,team:teamPage,rules:rulesPage,tags:tagsPage}[state.page]||overviewPage;root.innerHTML=await fn();wirePage();bindRecommendation()}catch(e){root.innerHTML=`<div class="empty-state error-state"><div class="empty-icon">!</div><h3>页面暂时无法加载</h3><p>${escapeHtml(e.message)}<br>请检查服务状态后重试。</p><button class="btn-primary" data-action="retry">重新加载</button></div>`;wirePage()}}
async function safeApi(path,fallback,label){try{return await api(path)}catch(e){console.error(`${label}加载失败`,e);return fallback}}
// Dashboard filters decide the query string used by both the page and the PDF
// export, so what is exported always matches what is on screen.
function dashboardQuery(){
 const params=[`range=${encodeURIComponent(state.overviewRange||'30d')}`];
 if(state.overviewPlatform)params.push(`platform=${encodeURIComponent(state.overviewPlatform)}`);
 if(state.overviewCategory)params.push(`category=${encodeURIComponent(state.overviewCategory)}`);
 return params.join('&');
}
function filterOptions(records,field){return [...new Set(records.map(record=>record[field]).filter(Boolean))].sort()}
function changeBadge(value,{suffix='%'}={}){
 if(value==null||!Number.isFinite(Number(value)))return '<span class="kpi-change muted">无可比数据</span>';
 const numeric=Number(value);
 const direction=numeric===0?'flat':(numeric>0?'up':'down');
 const label=numeric===0?'持平':`${numeric>0?'+':''}${formatDecimal(numeric)}${suffix}`;
 return `<span class="kpi-change ${direction} ${direction==='down'?'bad':'good'}">${label}</span>`;
}
function kpiCard(label,value,foot,change){
 return `<article class="kpi-card"><div class="kpi-label">${escapeHtml(label)}</div><div class="kpi-value">${value}</div><div class="kpi-foot">${change||''}<span>${foot}</span></div></article>`;
}
function trendPoints(series){
 const points=asArray(series);
 if(!points.length)return '';
 const maxViews=Math.max(1,...points.map(point=>num(point.total_views)));
 const maxInteractions=Math.max(1,...points.map(point=>num(point.total_interactions)));
 const position=index=>points.length===1?150:30+index*250/Math.max(points.length-1,1);
 const line=field=>points.map((point,index)=>`${position(index)},${140-num(point[field])/(field==='total_views'?maxViews:maxInteractions)*100}`).join(' ');
 return `<div class="trend-chart"><svg viewBox="0 0 300 175" preserveAspectRatio="none" role="img" aria-label="发布趋势折线图">${[40,73,106,140].map(y=>`<line x1="24" x2="292" y1="${y}" y2="${y}" class="chart-gridline"/>`).join('')}<polyline points="${line('total_views')}" class="chart-line"/><polyline points="${line('total_interactions')}" class="chart-line red"/>${points.map((point,index)=>`<circle cx="${position(index)}" cy="${140-num(point.total_views)/maxViews*100}" r="3" class="chart-point"><title>${escapeHtml(point.bucket)} · ${fmt(point.total_views)} 播放 · ${fmt(point.total_interactions)} 互动</title></circle>`).join('')}<text x="24" y="164" class="chart-axis">${escapeHtml(points[0].bucket)}</text><text x="292" y="164" class="chart-axis" text-anchor="end">${escapeHtml(points[points.length-1].bucket)}</text></svg></div><p class="chart-unit">纵轴：播放量与互动量各自按本区间峰值缩放 · 单位：次</p>`;
}
function dashboardBars(rows,valueOf,labelOf){
 const items=asArray(rows);
 if(!items.length)return '<div class="chart-empty">当前筛选条件下暂无已发布内容</div>';
 const max=Math.max(1,...items.map(row=>num(valueOf(row))));
 return `<div class="platform-list">${items.map(row=>`<div class="platform-row"><span class="platform-name">${escapeHtml(labelOf(row))}</span><div class="bar-track"><div class="bar-fill" style="width:${Math.min(100,num(valueOf(row))/max*100)}%"></div></div><span class="platform-count">${fmt(valueOf(row))}</span></div>`).join('')}</div>`;
}
async function overviewPage(){
 const [dashboard,adoption,diagnostics]=await Promise.all([
  api(`/api/stats/dashboard?${dashboardQuery()}`),
  safeApi('/api/stats/adoption',{adopted:{avg_completion:null,sample_count:0},not_adopted:{avg_completion:null,sample_count:0},completion_difference:null},'采纳统计'),
  safeApi('/api/stats/diagnostics',{items:[]},'运营提醒'),
 ]);
 const current=dashboard.current||{},changes=dashboard.changes||{};
 const filters=`<div class="dashboard-controls"><label class="form-field"><span>时间范围</span><select id="overviewRange">${[['7d','近 7 天'],['30d','近 30 天'],['90d','近 90 天'],['all','全部历史']].map(([value,label])=>`<option value="${value}" ${state.overviewRange===value?'selected':''}>${label}</option>`).join('')}</select></label><label class="form-field"><span>发布平台</span><select id="overviewPlatform"><option value="">全部平台</option>${filterOptions(state.records,'platform').map(value=>`<option ${value===state.overviewPlatform?'selected':''}>${escapeHtml(value)}</option>`).join('')}</select></label><label class="form-field"><span>内容类型</span><select id="overviewCategory"><option value="">全部类型</option>${CATEGORIES.map(value=>`<option ${value===state.overviewCategory?'selected':''}>${value}</option>`).join('')}</select></label><button class="btn-secondary" data-action="refresh-dashboard">刷新数据</button><button class="btn-primary" data-action="export-dashboard">导出 PDF</button></div>`;
 const workflow=asArray(dashboard.workflow);
 const total=workflow.reduce((sum,row)=>sum+num(row.count),0);
 const workflowPanel=`<section class="panel"><div class="panel-head"><div><h2 class="panel-title">内容与工作流分布</h2><div class="panel-subtitle">按当前筛选条件统计的内容数量；待制作与待排期反映待处理积压</div></div></div>${total?`<div class="workflow-grid">${workflow.map(row=>`<div class="workflow-card ${row.state===WORKFLOW.productionPending||row.state===WORKFLOW.schedulingPending?'attention':''}"><span>${escapeHtml(row.state)}</span><b>${fmt(row.count)}</b></div>`).join('')}</div>`:'<div class="chart-empty">暂无内容记录</div>'}</section>`;
 return `${heading('TEAM REVIEW · 01','运营总览','基于真实发布数据的团队经营看板；筛选条件同时作用于指标、图表与 PDF 导出')}
 ${filters}
 <div class="kpi-grid">${kpiCard('已发布内容',`${fmt(current.published_count)}<em> 条</em>`,`统计范围：${escapeHtml(dashboard.range_label||'')}`,changeBadge(changes.published_count_change))}${kpiCard('总播放量',fmt(current.total_views),`互动率 ${formatPercent(current.interaction_rate)}`,changeBadge(changes.total_views_change))}${kpiCard('总点赞量',fmt(current.total_likes),'来自已发布内容',changeBadge(changes.total_likes_change))}${kpiCard('总评论量',fmt(current.total_comments),'来自已发布内容',changeBadge(changes.total_comments_change))}${kpiCard('总收藏量',fmt(current.total_saves),'来自已发布内容',changeBadge(changes.total_saves_change))}${kpiCard('平均看完比例',formatPercent(current.avg_completion),`样本 ${fmt(current.published_count)} 条`,changeBadge(changes.avg_completion_change,{suffix:' 个百分点'}))}</div>
 <div class="grid dashboard-grid"><section class="panel"><div class="panel-head"><div><h2 class="panel-title">发布趋势</h2><div class="panel-subtitle">按时间汇总的发布数量、播放量与互动量</div></div><div class="chart-legend"><span><i class="legend-dot"></i>播放量</span><span><i class="legend-dot red"></i>互动量</span></div></div>${trendPoints(dashboard.trend)||'<div class="chart-empty">当前筛选条件下暂无已发布内容</div>'}</section><section class="panel"><div class="panel-head"><div><h2 class="panel-title">平台表现</h2><div class="panel-subtitle">按播放量排序的平台对比</div></div></div>${dashboardBars(dashboard.platforms,row=>row.total_views,row=>row.name)}<div class="table-wrap" style="margin-top:12px"><table class="data-table"><thead><tr><th>平台</th><th>发布数</th><th>互动率</th><th>平均看完</th></tr></thead><tbody>${asArray(dashboard.platforms).map(row=>`<tr><td>${escapeHtml(row.name)}</td><td>${fmt(row.published_count)}</td><td>${formatPercent(row.interaction_rate)}</td><td>${formatPercent(row.avg_completion)}</td></tr>`).join('')||'<tr><td colspan="4">暂无平台数据</td></tr>'}</tbody></table></div></section></div>
 <div class="grid dashboard-grid">${workflowPanel}<section class="panel"><div class="panel-head"><div><h2 class="panel-title">内容类型表现</h2><div class="panel-subtitle">按播放量排序的类型对比</div></div></div>${dashboardBars(dashboard.categories,row=>row.total_views,row=>row.name)}<div class="table-wrap" style="margin-top:12px"><table class="data-table"><thead><tr><th>内容类型</th><th>发布数</th><th>互动量</th></tr></thead><tbody>${asArray(dashboard.categories).map(row=>`<tr><td>${escapeHtml(row.name)}</td><td>${fmt(row.published_count)}</td><td>${fmt(row.total_interactions)}</td></tr>`).join('')||'<tr><td colspan="3">暂无类型数据</td></tr>'}</tbody></table></div></section></div>
 <div class="grid dashboard-grid"><section class="panel"><div class="panel-head"><div><h2 class="panel-title">采纳建议后的表现</h2><div class="panel-subtitle">对比采纳与未采纳记录，样本少时谨慎解读</div></div></div><div class="adoption-grid"><div><small>采纳建议</small><b>${formatPercent(adoption.adopted.avg_completion)}</b><span>${num(adoption.adopted.sample_count)} 条</span></div><div><small>未采纳</small><b>${formatPercent(adoption.not_adopted.avg_completion)}</b><span>${num(adoption.not_adopted.sample_count)} 条</span></div></div><p class="form-help">${adoption.completion_difference==null?'完成采纳排期并回填实际数据后，会显示两组差异。':`采纳组平均看完比例 ${adoption.completion_difference>=0?'高':'低'} ${formatPercentagePoints(Math.abs(num(adoption.completion_difference)))} 个百分点。样本量：${num(adoption.adopted.sample_count)} vs ${num(adoption.not_adopted.sample_count)}。`}</p></section><section class="panel"><div class="panel-head"><div><h2 class="panel-title">运营节奏提醒</h2><div class="panel-subtitle">基于当前内容状态的提示</div></div></div>${asArray(diagnostics.items).length?asArray(diagnostics.items).map(item=>`<div class="insight"><span class="insight-symbol">${item.level==='good'?'✦':'!'}</span><div><p>${escapeHtml(item.message)}</p></div></div>`).join(''):'<div class="chart-empty">暂无提醒</div>'}</section></div>`;
}
async function recordsPage(){
 const all=state.records,editor=['owner','editor'].includes(state.user?.role),filtered=filteredRecords(all),pageCount=Math.max(1,Math.ceil(filtered.length/state.recordsPageSize));
 state.recordsPage=Math.min(state.recordsPage,pageCount);const start=(state.recordsPage-1)*state.recordsPageSize,rows=filtered.slice(start,start+state.recordsPageSize),selected=new Set(state.selectedRecords),visibleSelected=rows.filter(x=>selected.has(x.id)).length;
 const actions=`<button class="btn-secondary" data-action="export-content-csv">导出内容 CSV</button>${editor?'<button class="btn-secondary" data-action="open-performance">＋ 预订演出</button><button class="btn-primary" data-action="open-add">＋ 创建宣发内容</button>':''}`;
 const platforms=[...new Set(all.map(x=>x.platform).filter(Boolean))].sort(),categories=[...new Set(all.map(x=>x.category).filter(Boolean))].sort();
 const list=rows.length?`<div class="table-wrap"><table class="data-table records-table"><thead><tr>${editor?`<th><input type="checkbox" id="selectVisible" ${visibleSelected===rows.length&&rows.length?'checked':''} aria-label="选择本页"></th>`:''}<th>宣发内容</th><th>关联演出</th><th>计划/实际时间</th><th>类型与平台</th><th>发布状态</th><th>工作流状态</th><th>播放/互动</th><th>负责人</th>${editor?'<th>操作</th>':''}</tr></thead><tbody>${rows.map(x=>{const booking=(state.performances||[]).find(p=>p.id===x.performance_id);return `<tr>${editor?`<td><input type="checkbox" data-record-select="${x.id}" ${selected.has(x.id)?'checked':''} aria-label="选择 ${escapeHtml(x.title)}"></td>`:''}<td class="content-title-cell"><b>${escapeHtml(x.title)}</b><small>${x.tags?.map(t=>tag(t)).join(' ')||''}</small></td><td>${escapeHtml(booking?.title||'未关联演出')}</td><td>${escapeHtml(x.published_at.slice(0,16).replace('T',' '))}</td><td>${tag(x.category)} ${tag(x.platform,'platform-tag')}</td><td><span class="status-pill ${workflowAssetStatus(x)==='已发布'?'published':'unpublished'}">${workflowAssetStatus(x)}</span></td><td><span class="status-pill ${WORKFLOW_COLORS[x.workflow_state]||'workflow-unpublished'}">${escapeHtml(x.workflow_state||'未设置工作流')}</span></td><td>${workflowAssetStatus(x)==='已发布'?`${fmt(x.views)} 播放 · ${fmt(x.total_interactions)} 互动`:'待发布'}</td><td>${escapeHtml(x.owner||'未分配负责人')}</td>${editor?`<td><button class="btn-secondary" data-action="edit-record" data-id="${x.id}">详情 / 编辑</button> ${recordWorkflowAction(x)} <button class="btn-secondary" data-action="delete-record" data-id="${x.id}">删除</button></td>`:''}</tr>`}).join('')}</tbody></table></div>`:all.length?'<div class="chart-empty">没有符合当前筛选条件的记录，请调整搜索或筛选条件。</div>':editor?emptyState('还没有内容资产'):'<div class="chart-empty">暂无内容记录，请联系团队录入者添加资产。</div>';
 const publishedCount=all.filter(x=>x.workflow_state===WORKFLOW.published).length,unpublishedCount=all.filter(x=>x.workflow_state==='未发布').length;
 return `${heading('YOUR RECORDS · 02','内容记录','按标题、平台、状态和发布时间管理团队内容资产',actions)}
 <div class="tabs asset-status-tabs" role="tablist" aria-label="内容工作流状态"><button class="tab-btn ${state.recordsStatus===''?'active':''}" data-content-status="" role="tab" aria-selected="${state.recordsStatus===''}">全部 <span>${all.length}</span></button>${WORKFLOW_STATES.map(status=>`<button class="tab-btn ${state.recordsStatus===status?'active':''}" data-content-status="${status}" role="tab" aria-selected="${state.recordsStatus===status}">${status} <span>${all.filter(x=>x.workflow_state===status).length}</span></button>`).join('')}</div>
 <section class="panel records-filters"><div class="filter-grid"><label class="form-field"><span>搜索标题 / 标签 / 负责人</span><input id="recordsSearch" value="${escapeHtml(state.recordsSearch)}" placeholder="输入关键词"/></label><label class="form-field"><span>平台</span><select id="recordsPlatform"><option value="">全部平台</option>${platforms.map(x=>`<option ${x===state.recordsPlatform?'selected':''}>${escapeHtml(x)}</option>`).join('')}</select></label><label class="form-field"><span>内容类型</span><select id="recordsCategory"><option value="">全部类型</option>${categories.map(x=>`<option ${x===state.recordsCategory?'selected':''}>${escapeHtml(x)}</option>`).join('')}</select></label><label class="form-field"><span>发布状态</span><select id="recordsLifecycle"><option value="">全部</option><option ${state.recordsLifecycle===WORKFLOW.published?'selected':''}>已发布</option><option ${state.recordsLifecycle===WORKFLOW.unpublished?'selected':''}>未发布</option></select></label><label class="form-field"><span>Tag</span><select id="recordsTag"><option value="">全部 Tag</option>${(state.tags||[]).map(t=>`<option value="${escapeHtml(t.name)}" ${state.recordsTag===t.name?'selected':''}>${escapeHtml(t.name)} · ${t.count}</option>`).join('')}</select></label><label class="form-field"><span>负责人</span><select id="recordsOwner"><option value="">全部负责人</option>${(state.users||[]).map(u=>`<option value="${u.id}" ${String(u.id)===state.recordsOwner?'selected':''}>${escapeHtml(u.name)}</option>`).join('')}</select></label><label class="form-field"><span>排序</span><select id="recordsSort"><option value="published_at" ${state.recordsSort==='published_at'?'selected':''}>发布时间</option><option value="views" ${state.recordsSort==='views'?'selected':''}>播放量</option><option value="completion" ${state.recordsSort==='completion'?'selected':''}>看完比例</option><option value="title" ${state.recordsSort==='title'?'selected':''}>标题</option></select></label><label class="form-field"><span>顺序</span><select id="recordsOrder"><option value="desc" ${state.recordsOrder==='desc'?'selected':''}>降序</option><option value="asc" ${state.recordsOrder==='asc'?'selected':''}>升序</option></select></label></div><div class="filter-summary">共 ${all.length} 条记录 · 当前筛选 ${filtered.length} 条</div></section>
 <section class="panel performance-list"><div class="panel-head"><div><h2 class="panel-title">演出预订</h2><div class="panel-subtitle">演出与宣发内容通过独立关联字段连接</div></div>${editor?'<button class="btn-secondary" data-action="open-performance">＋ 预订演出</button>':''}</div><div class="performance-cards">${(state.performances||[]).map(p=>`<article class="performance-card"><b>${escapeHtml(p.title)}</b><small>${escapeHtml(p.starts_at.slice(0,16).replace('T',' '))} · ${escapeHtml(p.venue||'场地待定')}</small><span>${escapeHtml(p.booking_status)} · ${p.content_count} 条关联内容 · ${escapeHtml(p.owner||'未分配负责人')}</span></article>`).join('')||'<p class="hint">还没有演出预订记录。</p>'}</div></section>

 <section class="panel records-list"><div class="panel-head"><div><h2 class="panel-title">团队内容资产</h2><div class="panel-subtitle">${all.length} 条 · 分类：${RECORD_CATEGORIES.join(' / ')}</div></div></div>${editor?`<div class="bulk-toolbar"><b>已选 ${selected.size} 条</b><button class="btn-secondary" data-action="bulk-select-all">全选当前分类（${filtered.length}）</button><button class="btn-secondary" data-action="bulk-clear" ${selected.size?'':'disabled'}>取消选择</button>${workflowBulkActions(selected)}<button class="btn-secondary danger-action" data-action="bulk-delete" ${selected.size?'':'disabled'}>批量删除</button></div>`:''}${list}<div class="pagination"><span>第 ${state.recordsPage} / ${pageCount} 页</span><label>每页<select id="recordsPageSize"><option value="10" ${state.recordsPageSize===10?'selected':''}>10</option><option value="20" ${state.recordsPageSize===20?'selected':''}>20</option><option value="50" ${state.recordsPageSize===50?'selected':''}>50</option></select></label><div><button class="btn-secondary" data-action="records-prev" ${state.recordsPage<=1?'disabled':''}>上一页</button><button class="btn-secondary" data-action="records-next" ${state.recordsPage>=pageCount?'disabled':''}>下一页</button></div></div></section>
 <section class="panel" style="margin-top:15px"><div class="panel-head"><h2 class="panel-title">A/B 假设与结论</h2></div><form id="hypothesisForm" class="inline-form"><input name="statement" required placeholder="例如：开头先展示制作过程会提高看完比例"/><button class="btn-primary">记录假设</button></form><div id="hypothesisList" class="hypothesis-list">加载中…</div></section>`
}
function workflowAssetStatus(record){return record.workflow_state===WORKFLOW.published?WORKFLOW.published:WORKFLOW.unpublished}
// Category semantics mirror the backend (internal/domain/workflow.go): 待制作,
// 已制作, 待排期 and 已排期 match the workflow state, while 未发布 and 已发布 are
// derived views over the publication outcome. 未发布 is not a workflow state.
const RECORD_CATEGORIES=['全部',...WORKFLOW_STATES];
function matchesRecordCategory(record,category){if(!category||category==='全部')return true;if(category===WORKFLOW.unpublished||category===WORKFLOW.published)return record.lifecycle===category;return record.workflow_state===category}
function filteredRecords(records){let items=[...records];const q=state.recordsSearch.trim().toLocaleLowerCase();if(q)items=items.filter(x=>[x.title,x.platform,x.owner,x.notes,...(x.tags||[])].join(' ').toLocaleLowerCase().includes(q));if(state.recordsStatus)items=items.filter(x=>matchesRecordCategory(x,state.recordsStatus));if(state.recordsLifecycle)items=items.filter(x=>workflowAssetStatus(x)===state.recordsLifecycle);if(state.recordsTag)items=items.filter(x=>(x.tags||[]).includes(state.recordsTag));if(state.recordsPlatform)items=items.filter(x=>x.platform===state.recordsPlatform);if(state.recordsCategory)items=items.filter(x=>x.category===state.recordsCategory);if(state.recordsOwner)items=items.filter(x=>String(x.owner_user_id||'')===state.recordsOwner);const key=state.recordsSort,dir=state.recordsOrder==='asc'?1:-1;items.sort((a,b)=>{const av=a[key]??'',bv=b[key]??'';return (typeof av==='number'?av-bv:String(av).localeCompare(String(bv),'zh-CN'))*dir});return items}
async function renderTagsOnly(){const root=qs('#pageRoot');root.innerHTML=await tagsPage();wirePage()}
function ruleFormPayload(form){const raw=Object.fromEntries(new FormData(form));let ruleContent;try{ruleContent=JSON.parse(raw.rule_content||'{}')}catch{throw new Error('规则内容必须是有效 JSON')};if(!ruleContent||typeof ruleContent!=='object'||Array.isArray(ruleContent))throw new Error('规则内容必须是 JSON 对象');return {name:String(raw.name||'').trim(),rule_type:raw.rule_type,content_type:raw.content_type,workflow_state:raw.workflow_state,priority:Number(raw.priority),enabled:raw.enabled==='true',rule_content:JSON.stringify(ruleContent),notes:raw.notes||'',trigger:'content.save',logic:'all',conditions:[],scope:{team_id:state.teamId},actions:[]}}
function resetRuleForm(){const form=qs('#ruleForm');if(!form)return;form.reset();form.elements.rule_id.value='';form.elements.name.value='';form.elements.priority.value='100';form.elements.rule_content.value='{}';const result=qs('#rulePreviewResult');if(result)result.textContent=''}
function wireRuleAndTagPages(){
 const rf=qs('#ruleForm');if(rf)rf.onsubmit=async e=>{e.preventDefault();try{const payload=ruleFormPayload(rf),id=rf.elements.rule_id.value;await api(id?`/api/rules/${id}`:'/api/rules',{method:id?'PATCH':'POST',body:JSON.stringify(payload)});await render();toast(id?'规则已更新':'规则已创建')}catch(err){toast(err.message)}};
 const tf=qs('#tagCreateForm');if(tf)tf.onsubmit=async e=>{e.preventDefault();try{await api('/api/tags',{method:'POST',body:JSON.stringify(Object.fromEntries(new FormData(tf)))});await render();toast('Tag 已加入团队库')}catch(err){toast(err.message)}}
}
async function runRulePreview(){const form=qs('#ruleForm');if(!form)return;const target=qs('#rulePreviewResult');try{const payload=ruleFormPayload(form),result=await api('/api/rules/preview',{method:'POST',body:JSON.stringify({rule:payload,text:qs('#rulePreviewText')?.value||'',tags:(qs('#rulePreviewTags')?.value||'').split(/[,，]/).map(x=>x.trim()).filter(Boolean)})});if(target)target.textContent=result.matched?`未通过：${result.result}（保存时会拒绝该内容）`:'校验通过（试运行只校验这一条规则）'}catch(err){if(target)target.textContent=`试运行失败：${err.message}`}}
async function runExtraAction(actionName,button){
 if(actionName==='manual-schedule'){const content=state.records.find(x=>x.id===button.dataset.id);if(content)await openManualSchedule(content);return true}
 if(actionName==='advance-workflow'){await advanceSingleRecord(button.dataset.id);return true}
 if(actionName==='rule-preview'){await runRulePreview();return true}
 if(actionName==='rule-clear'){resetRuleForm();return true}
 if(actionName==='rule-edit'){const item=state.rules.find(x=>String(x.id)===button.dataset.id),form=qs('#ruleForm');if(!item||!form)return true;form.elements.rule_id.value=item.id;for(const key of ['name','rule_type','content_type','workflow_state','priority','enabled','notes'])form.elements[key].value=String(item[key]??'');form.elements.rule_content.value=prettyRuleContent(item.rule_content);form.scrollIntoView({behavior:'smooth',block:'center'});return true}
 if(actionName==='rule-toggle'){const item=state.rules.find(x=>String(x.id)===button.dataset.id);if(item){await api(`/api/rules/${item.id}`,{method:'PATCH',body:JSON.stringify({enabled:!item.enabled})});await render();toast('规则状态已更新')}return true}
 if(actionName==='rule-delete'){const rule=state.rules.find(x=>String(x.id)===button.dataset.id);await confirmDialog({title:'删除校验规则',description:'删除后该规则不再参与内容保存校验，此操作不可撤销。',details:rule?[rule.name]:[],confirmLabel:'删除规则',danger:true,successMessage:'规则已删除',onConfirm:async()=>{await api(`/api/rules/${button.dataset.id}`,{method:'DELETE'});await render()}});return true}
 if(actionName==='tag-save'){const name=qs(`[data-tag-name="${button.dataset.id}"]`)?.value;await api(`/api/tags/${button.dataset.id}`,{method:'PATCH',body:JSON.stringify({name})});await render();toast('Tag 已更新');return true}
 if(actionName==='tag-toggle'){const active=button.dataset.active==='true';await api(`/api/tags/${button.dataset.id}`,{method:'PATCH',body:JSON.stringify({active:!active})});await render();toast(active?'Tag 已停用':'Tag 已启用');return true}
 if(actionName==='tag-merge'){const source=state.tags.find(t=>String(t.id)===String(button.dataset.id));if(source)await tagMergeDialog(source);await render();return true}
 if(actionName==='tag-prev'||actionName==='tag-next'){state.tagPage=Math.max(1,(state.tagPage||1)+(actionName==='tag-prev'?-1:1));await renderTagsOnly();return true}
 return false
}


async function analysisPage(){const [cat,heat,bench,adoption]=await Promise.all([api('/api/stats/category'),api('/api/stats/heatmap'),api('/api/benchmark'),api('/api/stats/adoption')]);const catStats=(cat&&typeof cat==='object')?cat:{},catItems=asArray(catStats.items),benchRows=asArray(bench),weights=catStats.weights||{completion:0.6,interaction:0.4};const min=num(catStats.min_sample,3);const cells=asArray(heat&&heat.items);const has=state.records.length>0;return `${heading('CONTENT REVIEW · 03','效果分析','多条件效果对比和团队自身基准，不代表行业平均',`<button class="btn-secondary" data-action="export-anonymized">导出脱敏 CSV</button>`)}${!has?emptyState():''}<div class="grid analysis-top">${statCard('纳入复盘',`${state.records.length}<em> 条</em>`,`当前样本门槛 ${min} 条`,'▤')}${statCard('平均看完比例',state.records.length>=min?formatPercent(state.overview.avg_completion):'样本不足',sampleLabel(state.records.length,min),'◷')}${statCard('采纳组看完比例',formatPercent(adoption.adopted.avg_completion),`${num(adoption.adopted.sample_count)} 条采纳记录`,'✳')}</div><div class="grid overview-grid"><section class="panel"><div class="panel-head"><div><h2 class="panel-title">内容类型表现</h2><div class="panel-subtitle">样本低于 ${min} 条时不展示均值；当前数据仅反映团队自身</div></div></div><div class="table-wrap"><table class="data-table"><thead><tr><th>内容类型</th><th>记录数</th><th>平均播放</th><th>平均看完</th><th>互动比例</th><th>自身基准</th></tr></thead><tbody>${CATEGORIES.map(name=>{const x=catItems.find(i=>i.name===name),b=benchRows.find(i=>i.category===name);return `<tr><td>${name}</td><td>${num(x?.sample_count)}</td><td>${x?.views==null?'样本不足':fmt(Math.round(num(x.views)))}</td><td>${x?.completion==null?'样本不足':formatPercent(x.completion)}</td><td>${x?.interaction_rate==null?'样本不足':formatPercent(x.interaction_rate)}</td><td>${b?.completion_median==null?'样本不足':formatPercent(b.completion_median)+' · '+(b.source_label||'自身历史')} <button class="btn-secondary" data-action="edit-benchmark" data-category="${name}" data-value="${b?.completion_median??''}" data-interaction="${b?.interaction_median??''}">调整</button></td></tr>`}).join('')}</tbody></table></div><div class="insight"><span class="insight-symbol">✦</span><div><b>综合表现分的计算方式</b><p>看完比例 × 完播权重 + 互动比例 × 互动权重。当前权重 ${formatDecimal(weights.completion)} / ${formatDecimal(weights.interaction)}，可在模型接口设置页调整。</p></div></div></section><section class="panel"><div class="panel-head"><div><h2 class="panel-title">星期与时段表现</h2><div class="panel-subtitle">低于样本门槛时留空，避免把偶然结果当规律</div></div></div><div class="heatmap"><div class="heat-head">星期 / 时段</div>${['上午','午间','下午','晚间','夜间'].map(x=>`<div class="heat-head">${x}</div>`).join('')}${WEEKDAYS.map((d,di)=>`<div class="heat-head">${d}</div>${['上午','午间','下午','晚间','夜间'].map(slot=>{const x=cells.find(i=>i.weekday===di&&i.time_slot===slot);return `<div class="heat-cell ${x?.completion!=null?'heat-'+Math.min(5,Math.max(1,Math.round(num(x.completion)/20))):'heat-1'}" title="${num(x?.sample_count)} 条样本">${x?.completion==null?'样本不足':formatPercent(x.completion)}</div>`}).join('')}`).join('')}</div></section></div><div class="grid overview-grid" style="margin-top:15px"><section class="panel"><h2 class="panel-title">转化漏斗</h2><div class="funnel-view">${await funnelMarkup()}</div></section><section class="panel"><h2 class="panel-title">评论倾向与关键词</h2>${await sentimentMarkup()}</section></div><section class="panel" style="margin-top:15px"><div class="panel-head"><div><h2 class="panel-title">用自然语言问团队数据</h2><div class="panel-subtitle">模型未配置或失败时，自动用规则统计回答</div></div></div><form id="insightForm" class="inline-form"><input name="question" required placeholder="例如：什么类型的内容看完比例较高？"/><button class="btn-primary">生成洞察</button></form><div id="insightResult"></div></section><section class="panel" style="margin-top:15px"><div class="panel-head"><h2 class="panel-title">换届交接包</h2><button class="btn-primary" data-action="handover">导出 Markdown</button></div><p class="form-help">打包高优内容、复盘结论、排期经验与下一步待办，方便团队交接。</p></section>`}
async function funnelMarkup(){const f=await api('/api/stats/funnel');const steps=[['播放',f.views],['进主页',f.profile_visits],['关注',f.follows],['到场',f.attendance],['购票',f.tickets]],max=Math.max(1,f.views);return `<div class="funnel-bars">${steps.map(([label,n])=>`<div><span>${label}</span><div class="bar-track"><div class="bar-fill" style="width:${n?Math.max(2,n/max*100):0}%"></div></div><b>${fmt(n)}</b></div>`).join('')}</div><small class="disclaimer">按团队主动录入的转化数据统计；${f.sample_count} 条内容。</small>`}
async function sentimentMarkup(){const words={positive:new Set(['清晰','喜欢','精彩','漂亮','好看','有趣','打动','细致','期待','优秀','棒']),negative:new Set(['太长','看不懂','模糊','太贵','不清楚','无聊','赶','难','一般','不喜欢'])};const text=state.records.map(x=>x.comment_text||'').join(' ');const tokens=text.match(/[\u4e00-\u9fa5]{2,6}/g)||[];const counts=new Map();tokens.forEach(t=>counts.set(t,(counts.get(t)||0)+1));const top=[...counts].sort((a,b)=>b[1]-a[1]).slice(0,8);const pos=[...words.positive].filter(w=>text.includes(w)).length,neg=[...words.negative].filter(w=>text.includes(w)).length;return `<p class="form-help">当前录入评论文本 ${text?1:0} 组；关键词使用本地词表粗略整理，不读取平台评论。</p><div class="sentiment"><span>正向词 ${pos}</span><span>需关注词 ${neg}</span></div><div class="keyword-list">${top.map(([w,n])=>tag(`${w} × ${n}`,'type-tag')).join(' ')||'暂无评论文本'}</div><small class="disclaimer">只分析团队主动录入的评论摘要，不采集观众个人信息。</small>`}
async function suggestionsPage(){
 let historyError='',history=[];
 try{history=asArray(await api('/api/recommendations'))}catch(err){historyError=(err&&err.message)?err.message:'接口异常'}
 if(!state.recommendation&&history.length)state.recommendation=history[0];
 await loadScheduleWindow();
 const boxContent=historyError?`<div class="empty-state error-state"><div class="empty-icon">!</div><h3>排期建议加载失败</h3><p>${escapeHtml(historyError)}<br>请检查服务状态后重试。</p><button class="btn-primary" data-action="retry">重新加载</button></div>`:(state.recommendation?recommendationView(state.recommendation):suggestionEmptyMarkup(null));
 return `${heading('NEXT WEEK · 04','排期建议','仅为「未发布 + 已制作」内容推荐；分析样本严格限定为同团队、同类型和同平台已发布内容',`<button class="btn-secondary" data-action="generate-rules">使用规则引擎</button><button class="btn-primary" data-action="generate">✳ AI 优先生成</button>`)}<div class="settings-note">建议可调整或取消后采纳。只有已制作内容会进入建议；样本不足会标注限制，不生成虚构结论；AI 最长等待 60 秒后使用规则引擎。</div>${scheduleContextMarkup()}<div id="recommendationBox" style="margin-top:15px">${boxContent}</div>`}
async function ensureSchedulePolicy(){if(state.schedulePolicy)return state.schedulePolicy;try{state.schedulePolicy=await api('/api/schedule/policy')}catch(e){console.error('排期规则集加载失败',e);state.schedulePolicy=null}return state.schedulePolicy}
async function loadScheduleWindow(){const policy=await ensureSchedulePolicy();state.scheduleWindow={start:policy?pureDateKey(policy.window_start):'',end:policy?pureDateKey(policy.window_end):''};state.scheduleWindowRows=[];if(state.scheduleWindow.start&&state.scheduleWindow.end){try{state.scheduleWindowRows=asArray(await api(`/api/schedule?start=${state.scheduleWindow.start}&end=${state.scheduleWindow.end}`))}catch(e){console.error('排期数据加载失败',e)}}return state.scheduleWindowRows}
function scheduleContextMarkup(){const policy=state.schedulePolicy;if(!policy)return '<div class="settings-note">未能读取只读排期规则集，采纳前的校验将以服务端结果为准。</div>';return `<div class="settings-note">当前规则集：${escapeHtml(policy.horizon_definition||'planning_horizon_days 为从今天起（含今天）共 N 个自然日')} · 可排期 ${escapeHtml(policy.window_start||'—')} 至 ${escapeHtml(policy.window_end||'—')} · 允许时间窗 ${escapeHtml(policy.time_windows.join('、')||'未配置')} · 每日上限 ${num(policy.max_per_day)} 条 · 每平台每周 ${num(policy.max_per_platform_per_week)} 条</div>`}
function suggestionCheck(item){if(!item)return {adoptable:false,reschedulable:false,reason:'建议不存在',conflict:null};if(item.adopted)return {adoptable:false,reschedulable:false,reason:'该建议已采纳并写入日历',conflict:null};if(item.invalid_reason)return {adoptable:false,reschedulable:false,reason:item.invalid_reason,conflict:null};if(!item.target_content_id)return {adoptable:false,reschedulable:false,reason:'建议缺少目标内容',conflict:null};if(!item.planned_date)return {adoptable:false,reschedulable:false,reason:'计划日期格式无效',conflict:null};const policy=state.schedulePolicy,window=state.scheduleWindow;if(window&&window.start&&window.end&&!withinWindow(item.planned_date,window.start,window.end))return {adoptable:false,reschedulable:false,reason:'计划日期超出规则集排期周期',conflict:null};if(policy&&policy.time_windows.length&&!policy.time_windows.includes(item.time_slot))return {adoptable:false,reschedulable:false,reason:'计划时间不符合规则集允许时间窗',conflict:null};if(policy&&policy.weekdays.length&&!policy.weekdays.includes(weekdayLabelOf(item.planned_date)))return {adoptable:false,reschedulable:false,reason:'计划日期不符合规则集允许星期',conflict:null};const conflict=asArray(state.scheduleWindowRows).find(row=>row.content_id&&row.content_id===item.target_content_id&&row.status==='待发布');if(conflict)return {adoptable:false,reschedulable:true,reason:`该内容已有待发布排期（${conflict.planned_date} ${conflict.planned_time}），可用本条建议调整原排期`,conflict};return {adoptable:true,reschedulable:false,reason:'',conflict:null}}
function suggestionStateMarkup(item,index){const check=suggestionCheck(item);if(item.adopted)return `<span class="status-pill published">已采纳</span><small class="form-help">该内容已加入日历${item.schedule_id?'（排期 '+escapeHtml(String(item.schedule_id).slice(0,8))+'…）':''}，其他建议保持原状。</small>`;if(check.reschedulable)return `<span class="status-pill unpublished">已有排期</span><small class="form-help">${escapeHtml(check.reason)}</small><button class="btn-secondary" data-action="adopt-reschedule" data-index="${index}">改为该建议日期</button>`;if(!check.adoptable)return `<span class="status-pill unpublished">不可采纳</span><small class="form-help">${escapeHtml(check.reason)}</small>`;return `<span class="status-pill published">可采纳</span><button class="btn-primary" data-action="adopt-one" data-index="${index}">采纳此排期</button>`}
function recommendationView(r){
 const sourceLabel=r.source_label||r.source||'本地规则引擎',items=asArray(r.recommendations),ready=items.filter(x=>suggestionCheck(x).adoptable).length;
 if(!items.length)return suggestionEmptyMarkup(r);
 return `<div class="insight" style="margin-bottom:13px"><span class="insight-symbol">✦</span><div><b>${escapeHtml(sourceLabel)}</b><p>同团队相似内容样本 ${num(r.reference_count)} 条 · 状态 ${escapeHtml(r.analysis_status||'completed')} · ${escapeHtml(r.confidence_note||'结论受样本量限制')}${r.fallback_reason?`<br>${escapeHtml(r.fallback_reason)}`:''}</p></div></div><div class="grid suggestion-grid">${items.map((x,i)=>`<article class="panel suggestion-card"><div class="suggestion-top"><div class="day-badge"><b>${escapeHtml(x.day_of_week||weekdayLabelOf(x.planned_date)||'待定')}</b><small>${escapeHtml(x.time_slot||'待定')}</small></div><div class="suggestion-copy"><h3 contenteditable="true" data-rec-title="${i}">${escapeHtml(x.title)}</h3><p>${tag(x.content_type)} · ${tag(x.platform,'platform-tag')}</p></div></div><div class="suggestion-meta"><span>目标内容 ${escapeHtml(x.title||'未指定标题')}</span><span>计划日期 ${escapeHtml(x.planned_date||'未确定')}</span><span>来源 ${escapeHtml(x.suggestion_source||sourceLabel)}</span><span>样本 ${num(x.reference_sample_count)} 条 · ${escapeHtml(x.confidence||'未标注')}</span></div><div class="reason-box">${escapeHtml(x.reason)}<br><small>${escapeHtml(x.limitations||'')}</small><br><small>内部内容 ID：${escapeHtml(x.target_content_id||'未提供')}</small></div><div class="suggestion-meta"><input data-rec-date="${i}" type="date" value="${escapeHtml(x.planned_date||'')}"/><input class="time-input" data-rec-time="${i}" type="time" value="${escapeHtml(x.time_slot||'')}"/><button class="btn-secondary" data-action="edit-suggestion" data-index="${i}">调整</button><button class="btn-secondary" data-action="cancel-suggestion" data-index="${i}">取消此建议</button></div><div class="suggestion-meta">${suggestionStateMarkup(x,i)}</div></article>`).join('')}</div><div class="form-actions"><button class="btn-primary" data-action="adopt" data-id="${r.id}" ${ready?'':'disabled title="当前没有可采纳的建议"'}>采纳剩余建议并加入日历（${ready} 条）</button></div>`
}
async function calendarPage(){const anchor=monthAnchor(new Date(),state.monthOffset),dates=monthGridDates(anchor),range=monthRange(anchor);state.calendar=asArray(await api(`/api/schedule?start=${range.first}&end=${range.last}`));state.scheduleWindowRows=state.calendar;const title=`${anchor.getFullYear()}年 ${pad(anchor.getMonth()+1)}月`,monthIndex=anchor.getMonth();const monthItems=[...state.calendar].sort((a,b)=>String(a.planned_date).localeCompare(String(b.planned_date))||String(a.planned_time).localeCompare(String(b.planned_time)));return `${heading('PUBLISHING PLAN · 05','排期日历','按月查看计划、负责人和发布状态',`<button class="btn-secondary" data-action="export-calendar">导出 CSV</button>`)}<section class="panel"><div class="panel-head"><div class="calendar-toolbar"><button class="btn-secondary" data-action="week-prev">‹ 上月</button><button class="btn-secondary" data-action="week-next">下月 ›</button><h2 class="week-label">${title}</h2><button class="btn-secondary" data-action="week-today">本月</button><div class="tabs"><button class="tab-btn ${state.calendarMode==='month'?'active':''}" data-mode="month">月历</button><button class="tab-btn ${state.calendarMode==='list'?'active':''}" data-mode="list">月清单</button></div></div></div>${state.calendarMode==='month'?`<div class="calendar-grid month-grid">${['周一','周二','周三','周四','周五','周六','周日'].map(d=>`<div class="month-weekday">${d}</div>`).join('')}${dates.map(d=>{const key=localDateKey(d),items=d.getMonth()===monthIndex?state.calendar.filter(e=>pureDateKey(e.planned_date)===key):[];return `<div class="calendar-day ${d.getMonth()!==monthIndex?'outside-month':''}" data-day="${key}"><div class="calendar-day-head"><b>${d.getDate()}</b></div>${items.map(e=>scheduleEvent(e)).join('')}</div>`}).join('')}</div>`:`<div class="calendar-list">${monthItems.map(e=>`<div class="calendar-list-row"><time>${escapeHtml(pureDateKey(e.planned_date)||'日期未知')} ${escapeHtml(e.planned_time||'')}</time><b>${escapeHtml(e.title)}</b><span>${escapeHtml(e.category)} · ${escapeHtml(e.platform)} · ${escapeHtml(e.owner||'未分配')} · ${escapeHtml(e.status)} ${['owner','editor'].includes(state.user?.role)?`<button class="btn-secondary" data-action="edit-schedule" data-id="${e.id}">调整</button> ${e.status==='已复盘'?'已回填':`<button class="btn-primary" data-action="publish" data-id="${e.id}">标记发布</button>`}`:''}</span></div>`).join('')||'<p class="hint">本月还没有排期。先生成并采纳一组建议。</p>'}</div>`}<p class="disclaimer" style="margin-top:12px">计划可以调整为规则集允许的日期与时间窗；回填发布数据后会同步进入内容资产并参与复盘。</p></section>`}

function scheduleEvent(e){const editor=['owner','editor'].includes(state.user?.role);return `<div class="calendar-event ${e.category==='技艺展示'?'alt':''}" ${editor?'draggable="true"':''} data-schedule-id="${e.id}" title="${editor?'拖动到其他日期可改期':'排期记录'}"><b>${escapeHtml(e.title)}</b><small>${e.planned_time} · ${escapeHtml(e.platform)} · ${escapeHtml(e.owner||'未分配')}</small><small>${escapeHtml(e.source_label|| (e.source==='manual'?'用户手动':'系统自动'))} · 操作人 ${escapeHtml(e.operator||'未知')}</small><small class="schedule-state">${escapeHtml(e.status)}</small>${editor?`<div class="event-actions"><button data-action="edit-schedule" data-id="${e.id}">调整</button>${e.status==='已复盘'?'已复盘':`<button data-action="publish" data-id="${e.id}">回填</button>`}</div>`:''}</div>`}
// Single-record workflow action: one step of the pipeline, or manual scheduling
// while the record waits for a plan. 已发布 records have no further step.
function recordWorkflowAction(record){
 const next=nextWorkflowState(record.workflow_state);
 if(record.workflow_state===WORKFLOW.schedulingPending)return `<button class="btn-secondary" data-action="manual-schedule" data-id="${record.id}">手动排期</button>`;
 if(!next)return '<span class="hint">已完成发布</span>';
 return `<button class="btn-secondary" data-action="advance-workflow" data-id="${record.id}">推进到${next}</button>`;
}
function wirePage(){wireRuleAndTagPages();const tagSearch=qs('#recordTagSearch');if(tagSearch)tagSearch.oninput=()=>qsa('.tag-picker label').forEach(label=>label.hidden=!label.textContent.toLocaleLowerCase().includes(tagSearch.value.toLocaleLowerCase()));const tagFilter=qs('#tagSearch');if(tagFilter)tagFilter.oninput=()=>{state.tagSearch=tagFilter.value;state.tagPage=1;renderTagsOnly()};qsa('.nav-item[data-page]').forEach(b=>b.onclick=()=>go(b.dataset.page));qsa('[data-content-status]').forEach(b=>b.onclick=()=>{state.recordsStatus=b.dataset.contentStatus;state.recordsPage=1;renderRecordsOnly()});qsa('[data-goto]').forEach(b=>b.onclick=()=>go(b.dataset.goto));qsa('[data-action]').forEach(b=>b.onclick=()=>action(b.dataset.action,b));qsa('[data-mode]').forEach(b=>b.onclick=()=>{state.calendarMode=b.dataset.mode;render()});wireRecordControls();const form=qs('#aiForm');if(form)form.onsubmit=saveAIConfig;for(const [id,key] of [['overviewRange','overviewRange'],['overviewPlatform','overviewPlatform'],['overviewCategory','overviewCategory']]){const el=qs('#'+id);if(el)el.onchange=()=>{state[key]=el.value;render()}};const af=qs('#analysisSettings');if(af)af.onsubmit=saveAnalysisSettings;const hf=qs('#hypothesisForm');if(hf)hf.onsubmit=addHypothesis;const inf=qs('#insightForm');if(inf)inf.onsubmit=generateInsight;const tf=qs('#teamForm');if(tf)tf.onsubmit=createTeam;qsa('[data-provider]').forEach(b=>b.onclick=()=>applyProvider(b.dataset.provider));const teamSel=qs('#teamSelect');if(teamSel)teamSel.onchange=()=>{state.teamId=Number(teamSel.value);localStorage.setItem('artsTeamId',state.teamId);render()};const roleSel=qs('#roleSelect');if(roleSel)roleSel.onchange=()=>{state.role=roleSel.value;localStorage.setItem('artsRole',state.role);render()};loadHypotheses();qsa('[data-rule-run]').forEach(b=>b.onclick=()=>runRules(b.dataset.ruleRun));const trf=qs('#teamRenameForm');if(trf)trf.onsubmit=async e=>{e.preventDefault();try{await api(`/api/teams/${state.teamId}`,{method:'PATCH',body:JSON.stringify(Object.fromEntries(new FormData(trf)))});await render();toast('团队信息已更新')}catch(err){toast(err.message)}};const uf=qs('#userForm');if(uf)uf.onsubmit=async e=>{e.preventDefault();try{await api('/api/users',{method:'POST',body:JSON.stringify(Object.fromEntries(new FormData(uf)))});await render();toast('成员账号已创建')}catch(err){toast(err.message)}};qsa('.calendar-day[data-day]').forEach(day=>{day.ondragover=e=>e.preventDefault();day.ondrop=async e=>{e.preventDefault();const id=e.dataTransfer.getData('text/plain');if(!id)return;try{await api(`/api/schedule/${id}`,{method:'PATCH',body:JSON.stringify({planned_date:day.dataset.day})});await render();toast('排期已改到 '+day.dataset.day)}catch(err){toast(err.message)}}});qsa('.calendar-event[data-schedule-id]').forEach(ev=>ev.ondragstart=e=>e.dataTransfer.setData('text/plain',ev.dataset.scheduleId))}
async function loadHypotheses(){const el=qs('#hypothesisList');if(!el)return;try{const rows=await api('/api/hypotheses');el.innerHTML=rows.map(x=>`<div class="hypothesis-row"><div><b>${escapeHtml(x.statement)}</b><small>${escapeHtml(x.status)} · ${escapeHtml(x.result||'尚未记录结论')}</small></div><button class="btn-secondary" data-action="hypothesis-status" data-id="${x.id}" data-status="${x.status}">更新状态</button></div>`).join('')||'<p class="hint">记录一个可验证的运营假设。</p>';qsa('[data-action="hypothesis-status"]',el).forEach(b=>b.onclick=async()=>{const status=b.dataset.status==='待验证'?'已验证':'待验证';await api(`/api/hypotheses/${b.dataset.id}`,{method:'PATCH',body:JSON.stringify({status})});loadHypotheses()})}catch(e){el.textContent=e.message}}
// Create/edit dialog: only the seven content fields are user-editable. The
// workflow state is assigned by the backend (new records start at 待制作) and the
// step data (素材说明/备注/Tag at 已制作, 发布指标 at 已发布) is collected by the
// workflow dialogs instead.
function openRecordForm(record=null){
 const x={platform:record?.platform||'',published_at:record?.published_at||new Date().toISOString().slice(0,16),title:record?.title||'',category:record?.category||'演出宣传',tags:(record?.tags||[]).join(',')||'',performance_id:record?.performance_id||'',owner_user_id:record?.owner_user_id||''};
 const label=record?'编辑内容资产':'创建宣发内容';
 showModal(`<div class="modal-head"><div><h2>${label}</h2><p>${record?'只更新你修改的字段；状态变更请使用工作流操作':'新内容将从「待制作」开始，其余信息可在工作流推进时补充'}</p></div><button class="modal-close" data-action="close" aria-label="关闭">×</button></div><form id="recordForm" class="modal-form"><div class="form-grid"><div class="form-field"><label>发布平台</label><input name="platform" required list="platformSuggestions" value="${escapeHtml(x.platform)}" placeholder="例如：视频号"/><datalist id="platformSuggestions">${platformChoices().map(choice=>`<option value="${escapeHtml(choice)}"></option>`).join('')}</datalist></div><div class="form-field"><label>标题</label><input name="title" required value="${escapeHtml(x.title)}" maxlength="300"/></div><div class="form-field"><label>内容负责人</label><select name="owner_user_id"><option value="">未分配</option>${asArray(state.users).map(user=>`<option value="${user.id}" ${Number(x.owner_user_id)===user.id?'selected':''}>${escapeHtml(user.name)} · ${escapeHtml(user.role_label||user.role)}</option>`).join('')}</select></div><div class="form-field"><label>关联演出</label><select name="performance_id"><option value="">暂不关联</option>${asArray(state.performances).map(item=>`<option value="${escapeHtml(item.id)}" ${item.id===x.performance_id?'selected':''}>${escapeHtml(item.title)} · ${escapeHtml(String(item.starts_at||'').slice(0,10))}</option>`).join('')}</select></div><div class="form-field"><label>内容类型</label><select name="category">${CATEGORIES.map(category=>`<option ${category===x.category?'selected':''}>${category}</option>`).join('')}</select></div><div class="form-field"><label>Tag</label><input name="tags" value="${escapeHtml(x.tags)}" placeholder="多个 Tag 用逗号分隔"/></div><div class="form-field"><label>计划发布时间</label><input name="published_at" type="datetime-local" required value="${escapeHtml(x.published_at.slice(0,16))}"/></div></div><div class="form-actions"><button type="button" class="btn-secondary" data-action="close">取消</button><button class="btn-primary">${record?'保存修改':'创建内容'}</button></div></form>`);
 const form=qs('#recordForm');
 form.onsubmit=async e=>{
  e.preventDefault();
  const data=Object.fromEntries(new FormData(form));
  const payload={title:String(data.title||'').trim(),platform:String(data.platform||'').trim(),category:data.category,published_at:data.published_at,tags:String(data.tags||'').split(/[,，]/).map(value=>value.trim()).filter(Boolean),owner_user_id:data.owner_user_id?Number(data.owner_user_id):null,performance_id:data.performance_id||null};
  if(!payload.title||!payload.platform){toast('标题和发布平台为必填项');return}
  try{
   if(record){
    for(const key of Object.keys(payload))if(payload[key]===null||payload[key]==='')delete payload[key];
    payload.tags=String(data.tags||'').split(/[,，]/).map(value=>value.trim()).filter(Boolean);
    await api(`/api/contents/${record.id}`,{method:'PATCH',body:JSON.stringify(payload)});
   }else{
    await api('/api/contents',{method:'POST',body:JSON.stringify(payload)});
   }
   closeModal();
   await render();
   toast(record?'内容已更新':'宣发内容已创建，当前状态为待制作');
  }catch(err){toast(err.message)}
 };
}
function openPerformanceForm(){
 showModal(`<div class="modal-head"><h2>预订演出</h2><button class="modal-close" data-action="close">×</button></div><form id="performanceForm" class="modal-form"><div class="form-grid"><div class="form-field full"><label>演出名称</label><input name="title" required maxlength="300"/></div><div class="form-field"><label>场地</label><input name="venue"/></div><div class="form-field"><label>演出时间</label><input name="starts_at" type="datetime-local" required/></div><div class="form-field"><label>预订状态</label><select name="booking_status"><option>已确认</option><option>待确认</option></select></div><div class="form-field"><label>负责人</label><select name="owner_user_id"><option value="">未分配</option>${(state.users||[]).map(u=>`<option value="${u.id}">${escapeHtml(u.name)}</option>`).join('')}</select></div><div class="form-field full"><label>说明</label><textarea name="notes" rows="2"></textarea></div></div><div class="form-actions"><button type="button" class="btn-secondary" data-action="close">取消</button><button class="btn-primary">保存演出预订</button></div></form>`);
 qs('#performanceForm').onsubmit=async e=>{e.preventDefault();const data=Object.fromEntries(new FormData(e.currentTarget));data.owner_user_id=data.owner_user_id?Number(data.owner_user_id):null;try{await api('/api/performances',{method:'POST',body:JSON.stringify(data)});closeModal();await render();toast('演出预订已保存')}catch(err){toast(err.message)}}
}
function closeModal(){qs('#modalRoot').innerHTML='';restoreModalFocus()}
let modalFocusOrigin=null;
function rememberModalFocus(){modalFocusOrigin=document.activeElement instanceof HTMLElement?document.activeElement:null}
function restoreModalFocus(){const target=modalFocusOrigin;modalFocusOrigin=null;if(target&&document.contains(target))target.focus()}
function showModal(content){rememberModalFocus();qs('#modalRoot').innerHTML=`<div class="modal-backdrop"><div class="modal" role="dialog" aria-modal="true">${content}</div></div>`;qsa('[data-action="close"]').forEach(b=>b.onclick=closeModal)}
function showInfo(title,message){showModal(`<div class="modal-head"><h2>${title}</h2><button class="modal-close" data-action="close" aria-label="关闭">×</button></div><p>${message}</p><div class="form-actions"><button class="btn-primary" data-action="close">知道了</button></div>`);const close=qs('#modalRoot [data-action="close"]');if(close)close.focus()}
// Unified confirmation dialog. Business actions must use this instead of the
// browser-native confirm/prompt: it renders the product's modal, shows the
// affected scope, optionally collects the fields a workflow step owns, exposes a
// loading state, reports failures in place and closes itself only after the
// server confirms the change. Field values survive the loading re-render.
function confirmDialog(options={}){
 const {title='请确认操作',description='',details=[],fields=[],body='',readValues=null,initialValues={},confirmLabel='确认',cancelLabel='取消',danger=false,successMessage='',onConfirm=null}=options;
 return new Promise(resolve=>{
  const titleId='confirmDialogTitle';
  let busy=false,settled=false;
  let values={...initialValues};
  const finish=result=>{if(settled)return;settled=true;document.removeEventListener('keydown',onKeydown,true);closeModal();resolve(result)};
  function onKeydown(event){if(event.key!=='Escape')return;event.preventDefault();if(!busy)finish(false)}
  const fieldMarkup=field=>{
   const current=values[field.name]??'';
   const label=escapeHtml(field.label||field.name);
   if(field.type==='select')return `<label class="form-field"><span>${label}</span><select data-workflow-field="${field.name}" ${busy?'disabled':''}>${(field.options||[]).map(option=>`<option value="${escapeHtml(option.value)}" ${String(option.value)===String(current)?'selected':''}>${escapeHtml(option.label)}</option>`).join('')}</select></label>`;
   if(field.type==='textarea')return `<label class="form-field full"><span>${label}</span><textarea data-workflow-field="${field.name}" rows="2" placeholder="${escapeHtml(field.placeholder||'')}" ${busy?'disabled':''}>${escapeHtml(current)}</textarea></label>`;
   return `<label class="form-field"><span>${label}</span><input data-workflow-field="${field.name}" type="${field.type==='number'?'number':'text'}" ${field.type==='number'?'min="0"':''} value="${escapeHtml(current)}" placeholder="${escapeHtml(field.placeholder||'')}" ${busy?'disabled':''}/></label>`;
  };
  const paint=(error='')=>{
   const bodyHtml=typeof body==='function'?body(values):body;
   qs('#modalRoot').innerHTML=`<div class="modal-backdrop"><div class="modal confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="${titleId}"><div class="modal-head"><div><h2 id="${titleId}">${escapeHtml(title)}</h2>${description?`<p class="confirm-description">${escapeHtml(description)}</p>`:''}</div><button class="modal-close" data-confirm-cancel aria-label="关闭" ${busy?'disabled':''}>×</button></div>${details.length?`<ul class="confirm-details">${details.map(line=>`<li>${escapeHtml(line)}</li>`).join('')}</ul>`:''}${bodyHtml}${fields.length?`<div class="form-grid confirm-fields">${fields.map(fieldMarkup).join('')}</div>`:''}${error?`<p class="confirm-error" role="alert">${escapeHtml(error)}</p>`:''}<div class="form-actions"><button class="btn-secondary" data-confirm-cancel ${busy?'disabled':''}>${escapeHtml(cancelLabel)}</button><button class="btn-primary${danger?' danger-action':''}" data-confirm-ok ${busy?'disabled':''} aria-busy="${busy}">${busy?'处理中…':escapeHtml(confirmLabel)}</button></div></div></div>`;
   const cancel=qs('#modalRoot [data-confirm-cancel]');if(cancel)cancel.onclick=()=>{if(!busy)finish(false)};
   const ok=qs('#modalRoot [data-confirm-ok]');
   if(ok)ok.onclick=async()=>{
    if(busy)return;
    try{
     const collected=readValues?readValues(qs('#modalRoot')):readWorkflowFields(qs('#modalRoot'),fields);
     values={...values,...(collected||{})};
    }catch(err){paint(err&&err.message?err.message:'请检查填写内容');return}
    busy=true;paint();
    try{await onConfirm(values);busy=false;if(successMessage)toast(successMessage);finish(true)}
    catch(err){busy=false;paint(err&&err.message?err.message:'操作失败，请稍后重试')}};
   const focusTarget=busy?qs('#modalRoot [data-confirm-cancel]'):qs('#modalRoot [data-confirm-ok]');
   if(focusTarget)focusTarget.focus()};
  rememberModalFocus();paint();document.addEventListener('keydown',onKeydown,true)})}
function tagMergeDialog(source){
 const candidates=asArray(state.tags).filter(tag=>tag.id!==source.id&&tag.active);
 return new Promise(resolve=>{
  const titleId='tagMergeDialogTitle';
  let busy=false,settled=false;
  const finish=result=>{if(settled)return;settled=true;document.removeEventListener('keydown',onKeydown,true);closeModal();resolve(result)};
  function onKeydown(event){if(event.key!=='Escape')return;event.preventDefault();if(!busy)finish(false)}
  const paint=(error='')=>{
   qs('#modalRoot').innerHTML=`<div class="modal-backdrop"><div class="modal confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="${titleId}"><div class="modal-head"><div><h2 id="${titleId}">合并 Tag</h2><p class="confirm-description">「${escapeHtml(source.name)}」的内容与使用记录会合并到目标 Tag，原 Tag 随后删除。</p></div><button class="modal-close" data-confirm-cancel aria-label="关闭" ${busy?'disabled':''}>×</button></div>${candidates.length?`<div class="form-field"><label for="tagMergeTarget">目标 Tag</label><select id="tagMergeTarget" ${busy?'disabled':''}>${candidates.map(tag=>`<option value="${tag.id}">${escapeHtml(tag.name)} · ${num(tag.count)} 条内容</option>`).join('')}</select></div>`:'<p class="confirm-error" role="alert">没有可合并的其他启用 Tag。</p>'}${error?`<p class="confirm-error" role="alert">${escapeHtml(error)}</p>`:''}<div class="form-actions"><button class="btn-secondary" data-confirm-cancel ${busy?'disabled':''}>取消</button><button class="btn-primary" data-confirm-ok ${busy||!candidates.length?'disabled':''} aria-busy="${busy}">${busy?'处理中…':'确认合并'}</button></div></div></div>`;
   const cancel=qs('#modalRoot [data-confirm-cancel]');if(cancel)cancel.onclick=()=>{if(!busy)finish(false)};
   const ok=qs('#modalRoot [data-confirm-ok]');
   if(ok)ok.onclick=async()=>{
    if(busy||!candidates.length)return;
    const target=qs('#tagMergeTarget');
    busy=true;paint();
    try{await api(`/api/tags/${source.id}/merge`,{method:'POST',body:JSON.stringify({target_id:Number(target.value)})});busy=false;toast('Tag 已合并');finish(true)}
    catch(err){busy=false;paint(err&&err.message?err.message:'合并失败，请稍后重试')}};
   const focusTarget=busy?qs('#modalRoot [data-confirm-cancel]'):qs('#modalRoot [data-confirm-ok]');
   if(focusTarget)focusTarget.focus()};
  rememberModalFocus();paint();document.addEventListener('keydown',onKeydown,true)})}
function formAIData(){const data=Object.fromEntries(new FormData(qs('#aiForm')));try{data.platform_weights=data.platform_weights_json?JSON.parse(data.platform_weights_json):(state.settings.platform_weights||{})}catch{toast('平台权重模板不是有效 JSON');return null}delete data.platform_weights_json;data.temperature=Number(data.temperature);data.fallback_enabled=data.fallback_enabled==='true';data.enabled=true;return data}
async function saveAIConfig(e){e.preventDefault();const data=formAIData();if(!data)return;try{state.settings=await api('/api/settings',{method:'PUT',body:JSON.stringify(data)});await render();toast('模型设置已保存在后端')}catch(err){toast(err.message)}}
async function saveAnalysisSettings(e){e.preventDefault();const d=Object.fromEntries(new FormData(e.currentTarget));try{await api('/api/settings',{method:'PUT',body:JSON.stringify({...state.settings,...d,api_key:''})});await render();toast('分析口径已更新')}catch(err){toast(err.message)}}
function applyProvider(){const form=qs('#aiForm');form.elements.request_mode.value='base'}
async function createTeam(e){e.preventDefault();const name=new FormData(e.currentTarget).get('name');try{const t=await api('/api/teams',{method:'POST',body:JSON.stringify({name})});state.teamId=t.id;localStorage.setItem('artsTeamId',t.id);await render();toast('团队已创建并切换')}catch(err){toast(err.message)}}
async function generateInsight(e){e.preventDefault();const data=Object.fromEntries(new FormData(e.currentTarget));const out=qs('#insightResult');out.innerHTML='<p class="hint">正在整理团队记录…</p>';try{const res=await api('/api/ai/insight',{method:'POST',body:JSON.stringify(data)});out.innerHTML=`<div class="insight"><span class="insight-symbol">✦</span><div><b>${escapeHtml(res.source)} · 参考 ${res.sample_count} 条</b><p>${escapeHtml(res.answer)}</p></div></div>`}catch(err){out.textContent=err.message}}
async function addHypothesis(e){e.preventDefault();const d=Object.fromEntries(new FormData(e.currentTarget));try{await api('/api/hypotheses',{method:'POST',body:JSON.stringify(d)});e.currentTarget.reset();loadHypotheses();toast('假设已记录')}catch(err){toast(err.message)}}
async function action(a,b){
 try{
  if(await runExtraAction(a,b))return;
  if(a==='edit-benchmark'){editBenchmark(b);return}
  if(a==='run-rules'){await runRules(b.dataset.contentId);return}
  if(a==='retry'){await render();return}if(a==='refresh-dashboard'){await render();toast('已刷新运营数据');return}if(a==='export-dashboard'){downloadApi(`/api/report/export?${dashboardQuery()}`);return}
  if(a==='open-add'){openRecordForm();return}
  if(a==='open-performance'){openPerformanceForm();return}
  if(a==='edit-record'){const x=state.records.find(c=>c.id===b.dataset.id);openRecordForm(x);return}
  if(a==='delete-record'){const record=state.records.find(c=>c.id===b.dataset.id);await confirmDialog({title:'删除内容记录',description:'删除后该内容资产不再出现在列表、统计与复盘中，此操作不可撤销。',details:record?[record.title]:[],confirmLabel:'删除记录',danger:true,successMessage:'记录已删除',onConfirm:async()=>{await api(`/api/contents/${b.dataset.id}`,{method:'DELETE'});state.selectedRecords=state.selectedRecords.filter(id=>id!==b.dataset.id);await render()}});return}
  if(a==='export-content-csv'){downloadApi('/api/export/contents');return}
  if(a==='export-report'){downloadApi('/api/report/export');return}
  if(a==='export-anonymized'){downloadApi('/api/export/anonymized');return}
  if(a==='handover'){downloadApi('/api/handover/export?format=markdown');return}
  if(a==='audit'){const rows=asArray(await api('/api/audit'));showInfo('最近操作记录',rows.slice(0,20).map(x=>`${escapeHtml(dateTimeText(x.created_at))} · ${escapeHtml(x.actor)} · ${escapeHtml(x.action)} · ${escapeHtml(x.detail)}`).join('<br>')||'暂无操作记录');return}
  if(a==='test-ai'){toast('正在通过后端测试模型连通性…');const r=await api('/api/ai/test',{method:'POST'});toast(`${r.message} · ${r.elapsed_ms}ms`);return}
  if(a==='save-model-profile'){const data=formAIData();if(!data)return;const saved=await api('/api/ai/configs',{method:'POST',body:JSON.stringify(data)});await api(`/api/ai/configs/${saved.id}/activate`,{method:'POST'});await render();toast('新模型配置已保存并启用');return}
  if(a==='activate-model'){await api(`/api/ai/configs/${b.dataset.id}/activate`,{method:'POST'});await render();toast('已切换当前模型');return}
  if(a==='generate'||a==='generate-rules'){const box=qs('#recommendationBox');if(box)box.innerHTML='<div class="chart-empty loading-state">正在生成排期建议…</div>';if(b)b.disabled=true;toast(a==='generate'?'正在请求 AI 分析，最长等待 60 秒…':'正在使用本地规则引擎…');state.recommendation=await api(`/api/ai/recommend${a==='generate-rules'?'?force_rule=true':''}`,{method:'POST'});await loadScheduleWindow();refreshRecommendationBox();if(b)b.disabled=false;toast(state.recommendation.source_label);return}
  if(a==='edit-suggestion'){toast('修改日期、时间或标题后即可采纳');return}
  if(a==='cancel-suggestion'){const index=Number(b.dataset.index),items=currentEditedRecommendations(),previous=state.recommendation;items.splice(index,1);await api(`/api/recommendations/${previous.id}`,{method:'PATCH',body:JSON.stringify({recommendations:items})});await reloadRecommendations();toast('已移除该建议');return}
  if(a==='adopt-one'||a==='adopt-reschedule'||a==='adopt'){await adoptSuggestion(a,b);return}
  if(a==='week-prev'){state.monthOffset--;await render();return}
  if(a==='week-next'){state.monthOffset++;await render();return}
  if(a==='week-today'){state.monthOffset=0;await render();return}
  if(a==='records-prev'){state.recordsPage=Math.max(1,state.recordsPage-1);await renderRecordsOnly();return}
  if(a==='records-next'){state.recordsPage++;await renderRecordsOnly();return}
  if(a==='bulk-advance'){await bulkAdvanceWorkflow();return}
  if(a==='bulk-manual-schedule'){await manualScheduleBatchDialog();return}
  if(a==='bulk-select-all'){await selectAllFilteredRecords();return}
  if(a==='bulk-clear'){state.selectedRecords=[];await renderRecordsOnly();return}
  if(a==='bulk-delete'){const count=state.selectedRecords.length;await confirmDialog({title:'批量删除内容记录',description:`将删除已选择的 ${count} 条内容记录，删除后不再出现在列表、统计与复盘中。`,details:state.records.filter(x=>state.selectedRecords.includes(x.id)).slice(0,8).map(x=>x.title),confirmLabel:'批量删除',danger:true,successMessage:'已批量删除',onConfirm:async()=>{await bulkDeleteRecords()}});return}
  if(a==='export-calendar'){downloadApi('/api/schedule/export');return}
  if(a==='edit-schedule'){editSchedule(b.dataset.id);return}
  if(a==='publish'){publishSchedule(b.dataset.id);return}
 }catch(e){
  if(['generate','generate-rules'].includes(a)){setRecommendationError(e.message);if(b)b.disabled=false}
  toast(e.message)}}
function currentEditedRecommendations(){const r=state.recommendation;if(!r)return[];return asArray(r.recommendations).map((x,i)=>({...x,title:(qs(`[data-rec-title="${i}"]`)?.textContent||'').trim()||x.title,planned_date:pureDateKey(qs(`[data-rec-date="${i}"]`)?.value)||x.planned_date,time_slot:qs(`[data-rec-time="${i}"]`)?.value||x.time_slot}))}
function bindRecommendation(){qsa('#recommendationBox [data-action]').forEach(b=>b.onclick=()=>action(b.dataset.action,b));qsa('#recommendationBox [data-goto]').forEach(b=>b.onclick=()=>go(b.dataset.goto));qsa('#recommendationBox [data-rec-date],#recommendationBox [data-rec-time]').forEach(input=>input.onchange=()=>{const r=state.recommendation;if(!r)return;state.recommendation={...r,recommendations:currentEditedRecommendations()};refreshRecommendationBox()})}
function refreshRecommendationBox(){const box=qs('#recommendationBox');if(!box)return;box.innerHTML=state.recommendation?recommendationView(state.recommendation):suggestionEmptyMarkup(null);bindRecommendation()}
// Always read the newest suggestion data from the server instead of trusting a
// local copy; the token keeps a slow response from overwriting a newer one.
let recommendationReloadToken=0;
async function reloadRecommendations(){
 const token=++recommendationReloadToken;
 const history=asArray(await api('/api/recommendations'));
 if(token!==recommendationReloadToken)return state.recommendation;
 const currentId=state.recommendation&&state.recommendation.id;
 state.recommendation=history.find(x=>x.id===currentId)||history[0]||null;
 refreshRecommendationBox();
 return state.recommendation;
}
function setRecommendationError(message){const box=qs('#recommendationBox');if(!box)return;box.innerHTML=`<div class="empty-state error-state"><div class="empty-icon">!</div><h3>排期建议加载失败</h3><p>${escapeHtml(message||'接口异常')}<br>请检查服务状态后重试。</p><button class="btn-primary" data-action="generate-rules">重新生成建议</button></div>`;bindRecommendation()}
// Unpublished, finished content is the only source of schedule suggestions; the
// rule lives in lib/suggestions.ts so it stays aligned with the backend filter.
function readySuggestionCandidates(){return state.records.filter(x=>isReadySuggestionCandidate(x.lifecycle,x.workflow_state))}
// Empty state for the suggestion list. Only finished videos can be suggested, so
// "no candidates" and "nothing to show" both surface the same required wording.
function suggestionEmptyMarkup(r){
 const status=r?r.analysis_status:'';
 if(showNoFinishedVideoEmptyState(status,readySuggestionCandidates().length)){
  return `<div class="empty-state"><div class="empty-icon">❋</div><h3>${NO_FINISHED_VIDEO_TEXT}</h3><p>排期建议只针对「未发布 + 已制作」的内容。先在内容记录中把待制作内容推进到已制作，或在排期日历中调整占用中的排期后再生成建议。</p><button class="btn-secondary" data-goto="records">前往内容记录</button></div>`
 }
 return `<div class="chart-empty">${escapeHtml((r&&r.fallback_reason)||'当前没有可展示的排期建议。')}<br>分析状态：${escapeHtml(status||'no_candidates')}</div>`
}
function adoptPayload(item){return {target_content_id:item.target_content_id,planned_date:item.planned_date,time_slot:item.time_slot,title:item.title}}
// Adoption runs through the unified dialog whenever the user must confirm the
// scope (skipped or adjusted items); the server result is then re-read so the
// list always reflects persisted data.
async function adoptSuggestion(a,b){
 const previous=state.recommendation;if(!previous){toast('请先生成排期建议');return}
 const items=currentEditedRecommendations(),index=b&&b.dataset.index!=null?Number(b.dataset.index):null;
 const scoped=index==null?items:[items[index]].filter(Boolean);
 if(!scoped.length){toast('没有可处理的建议');return}
 const checks=scoped.map(item=>({item,check:suggestionCheck(item)}));
 const blocked=checks.filter(entry=>!entry.check.adoptable&&!entry.check.reschedulable);
 if(blocked.length&&a!=='adopt'){toast(`无法采纳：${blocked[0].check.reason}`);return}
 const fresh=checks.filter(entry=>entry.check.adoptable).map(entry=>entry.item),conflicts=checks.filter(entry=>entry.check.reschedulable);
 const outcome={created:0,adjusted:0};
 const run=async()=>{
  for(const {item,check} of conflicts){
   await api(`/api/schedule/${check.conflict.id}`,{method:'PATCH',body:JSON.stringify({planned_date:item.planned_date,planned_time:item.time_slot,title:item.title,category:item.content_type,platform:item.platform,owner:item.owner||check.conflict.owner})});
   outcome.adjusted++;
  }
  if(fresh.length){const result=await api('/api/schedule/adopt',{method:'POST',body:JSON.stringify({recommendation_id:previous.id,items:fresh.map(adoptPayload)})});outcome.created=num(result.created)}
  await loadScheduleWindow();
  await reloadRecommendations();
 };
 if(blocked.length||conflicts.length){
  const lines=blocked.slice(0,5).map(entry=>`${entry.item.title||entry.item.target_content_id}：${entry.check.reason}`)
   .concat(conflicts.slice(0,5).map(entry=>`${entry.item.title||entry.item.target_content_id}：已有排期（${entry.check.conflict.planned_date} ${entry.check.conflict.planned_time}），将改为建议时间`))
   .concat(blocked.length+conflicts.length>10?[`另有 ${blocked.length+conflicts.length-10} 条需要处理`]:[]);
  const confirmed=await confirmDialog({title:'确认排期建议的处理方式',description:`本次将新建 ${fresh.length} 条排期，调整 ${conflicts.length} 条已有排期，跳过 ${blocked.length} 条不可采纳建议。`,details:lines,confirmLabel:'执行',onConfirm:run});
  if(!confirmed)return;
 }else{
  if(b)b.disabled=true;
  await run();
 }
 if(a==='adopt'){await go('calendar');toast(`已写入 ${outcome.created} 条新排期，调整 ${outcome.adjusted} 条已有排期${blocked.length?`，跳过 ${blocked.length} 条不可采纳建议`:''}`);return}
 toast(outcome.created?`已采纳此排期（新建 ${outcome.created} 条）`:'已按建议调整已有排期，其他建议保持不变');
}
async function openManualSchedule(content){
 const policy=await ensureSchedulePolicy();
 const rows=state.scheduleWindowRows.length?state.scheduleWindowRows:await loadScheduleWindow();
 const existing=asArray(rows).find(x=>x.content_id===content.id&&x.status==='待发布');
 const initialDate=(existing&&existing.planned_date)||pureDateKey(content.published_at)||localDateKey(new Date());
 const timeWindows=asArray(policy&&policy.time_windows);
 const initialTime=(existing&&existing.planned_time)||timeWindows[0]||'19:30';
 const timeField=timeWindows.length?`<select name="planned_time">${timeWindows.map(t=>`<option ${t===initialTime?'selected':''}>${t}</option>`).join('')}</select>`:`<input name="planned_time" type="time" required value="${escapeHtml(initialTime)}"/>`;
 showModal(`<div class="modal-head"><div><h2>${existing?'调整手动排期':'手动排期'}</h2><p>${escapeHtml(content.title)} · 排期由当前登录用户负责记录</p></div><button class="modal-close" data-action="close">×</button></div><form id="manualScheduleForm" class="modal-form"><div class="form-grid"><div class="form-field"><label>发布时间</label><input name="planned_date" type="date" required value="${escapeHtml(initialDate)}"/></div><div class="form-field"><label>时间段</label>${timeField}</div></div><p class="form-help">手动排期会按排期周期、允许星期、时间窗和发布频率校验；已有排期将更新原记录并保留人工来源。${policy?'':'当前未读取到排期规则集，将以服务端校验结果为准。'}</p><div class="form-actions"><button type="button" class="btn-secondary" data-action="close">取消</button><button class="btn-primary">保存排期</button></div></form>`);
 qs('#manualScheduleForm').onsubmit=async e=>{e.preventDefault();const data=Object.fromEntries(new FormData(e.currentTarget));try{if(existing){await api(`/api/schedule/${existing.id}`,{method:'PATCH',body:JSON.stringify(data)})}else{await api('/api/schedule/manual',{method:'POST',body:JSON.stringify({content_id:content.id,planned_date:data.planned_date,planned_time:data.planned_time})})}closeModal();await loadScheduleWindow();await render();toast(existing?'排期已更新':'手动排期已保存')}catch(err){toast(err.message)}}
}
function editSchedule(id){const item=state.calendar.find(x=>x.id===id);if(!item)return;showModal(`<h2>调整排期</h2><p>保存后更新服务器日历。</p><form id="scheduleForm"><div class="form-grid"><div class="form-field"><label>计划日期</label><input name="planned_date" type="date" required value="${item.planned_date}"/></div><div class="form-field"><label>发布时间</label><input name="planned_time" type="time" required value="${item.planned_time}"/></div><div class="form-field full"><label>内容主题</label><input name="title" required value="${escapeHtml(item.title)}"/></div><div class="form-field"><label>内容类型</label><select name="category">${CATEGORIES.map(c=>`<option ${c===item.category?'selected':''}>${c}</option>`).join('')}</select></div><div class="form-field"><label>发布平台</label><input name="platform" value="${escapeHtml(item.platform)}" required/></div><div class="form-field"><label>负责人</label><input name="owner" value="${escapeHtml(item.owner||'')}"/></div></div><div class="form-actions"><button type="button" class="btn-secondary" data-action="close">取消</button><button class="btn-primary">保存调整</button></div></form>`);qs('#scheduleForm').onsubmit=async e=>{e.preventDefault();try{await api(`/api/schedule/${id}`,{method:'PATCH',body:JSON.stringify(Object.fromEntries(new FormData(e.currentTarget)))});closeModal();await render();toast('日历已更新')}catch(err){toast(err.message)}}}
function publishSchedule(id){const item=state.calendar.find(x=>x.id===id);if(!item)return;const plannedDate=pureDateKey(item.planned_date);showModal(`<h2>回填实际表现</h2><p>实际发布后记录数据，系统会对照预测范围并纳入经验库。</p><form id="publishForm"><div class="form-grid"><div class="form-field full"><label>实际发布标题</label><input name="title" value="${escapeHtml(item.title)}" required/></div><div class="form-field"><label>内容类型</label><select name="category">${CATEGORIES.map(c=>`<option ${c===item.category?'selected':''}>${c}</option>`).join('')}</select></div><div class="form-field"><label>平台</label><input name="platform" value="${escapeHtml(item.platform)}" required/></div><div class="form-field"><label>实际发布时间</label><input name="published_at" type="datetime-local" value="${plannedDate}T${escapeHtml(item.planned_time||'19:30')}" required/></div>${[['views','播放量'],['likes','点赞量'],['comments','评论量'],['saves','收藏量'],['shares','转发量'],['completion','看完比例（%）'],['profile_visits','主页访问'],['follows','新增关注'],['attendance','到场人数'],['tickets','售票数']].map(([k,l])=>`<div class="form-field"><label>${l}</label><input type="number" name="${k}" min="0" ${k==='completion'?'max="100"':''} required value="0"/></div>`).join('')}<div class="form-field full"><label>评论摘录</label><textarea name="comment_text" rows="2"></textarea></div></div><div class="form-actions"><button type="button" class="btn-secondary" data-action="close">取消</button><button class="btn-primary">保存实际数据</button></div></form>`);qs('#publishForm').onsubmit=async e=>{e.preventDefault();const data=Object.fromEntries(new FormData(e.currentTarget));for(const k of ['views','likes','comments','saves','shares','completion','profile_visits','follows','attendance','tickets'])data[k]=Number(data[k]);try{const res=await api(`/api/schedule/${id}/publish`,{method:'POST',body:JSON.stringify(data)});closeModal();await render();const diff=res&&res.prediction?res.prediction.views_deviation_percent:null;toast(diff==null?'已保存回填数据':`已保存，播放量偏差 ${formatPercent(diff)}`)}catch(err){toast(err.message)}}}
function wireRecordControls(){const search=qs('#recordsSearch');if(search)search.oninput=()=>{state.recordsSearch=search.value;state.recordsPage=1;const pos=search.selectionStart;renderRecordsOnly().then(()=>{const input=qs('#recordsSearch');input?.focus();input?.setSelectionRange(pos,pos)})};for(const [id,key] of [['recordsLifecycle','recordsLifecycle'],['recordsTag','recordsTag'],['recordsPlatform','recordsPlatform'],['recordsCategory','recordsCategory'],['recordsOwner','recordsOwner'],['recordsSort','recordsSort'],['recordsOrder','recordsOrder']]){const el=qs('#'+id);if(el)el.onchange=()=>{state[key]=el.value;state.recordsPage=1;renderRecordsOnly()}}const size=qs('#recordsPageSize');if(size)size.onchange=()=>{state.recordsPageSize=Number(size.value);state.recordsPage=1;renderRecordsOnly()};const selectVisible=qs('#selectVisible');if(selectVisible)selectVisible.onchange=()=>{const ids=qsa('[data-record-select]').map(x=>x.dataset.recordSelect);state.selectedRecords=selectVisible.checked?[...new Set([...state.selectedRecords,...ids])]:state.selectedRecords.filter(id=>!ids.includes(id));renderRecordsOnly()};qsa('[data-record-select]').forEach(box=>box.onchange=()=>{state.selectedRecords=box.checked?[...new Set([...state.selectedRecords,box.dataset.recordSelect])]:state.selectedRecords.filter(id=>id!==box.dataset.recordSelect);renderRecordsOnly()})}
async function renderRecordsOnly(){const root=qs('#pageRoot');root.innerHTML=await recordsPage();wirePage()}
async function bulkDeleteRecords(){
 try{
  const count=state.selectedRecords.length;
  await api('/api/contents/bulk',{method:'POST',body:JSON.stringify({ids:state.selectedRecords,action:'delete'})});
  state.selectedRecords=[];
  await render();
  toast(`已删除 ${count} 条内容记录`);
 }catch(e){toast(e.message)}
}
// Records that the batch workflow advance may touch: selected records that are
// still 待制作. Other states are never included, so the batch cannot move records
// the single-record rule does not allow.
// Counts of the current selection per workflow state.
function selectedStateCounts(selected){
 const counts={};
 for(const record of state.records){
  if(!selected.has(record.id))continue;
  counts[record.workflow_state]=(counts[record.workflow_state]||0)+1;
 }
 return counts;
}
// Bulk actions offered for the selected category. Only the transition that
// belongs to that category is available, so the workflow cannot be skipped.
function workflowBulkActions(selected){
 const counts=selectedStateCounts(selected),category=state.recordsStatus;
 const action=(name,label,state,hint)=>{
  const count=counts[state]||0;
  const title=count?hint:`当前选择中没有「${state}」内容`;
  return `<button class="btn-secondary" data-action="${name}" ${count?'':'disabled'} title="${escapeHtml(title)}">${label}${count?`（${count}）`:''}</button>`;
 };
 if(category===WORKFLOW.productionPending)return action('bulk-advance','批量推进工作流（已制作）',WORKFLOW.productionPending,'补充素材说明、Tag、备注后推进到已制作');
 if(category===WORKFLOW.productionComplete)return action('bulk-advance','批量推进工作流（待排期）',WORKFLOW.productionComplete,'补齐负责人、关联演出后推进到待排期');
 if(category===WORKFLOW.schedulingPending)return action('bulk-manual-schedule','批量手动排期',WORKFLOW.schedulingPending,'逐条设置发布平台与计划时间并推进到已排期');
 if(category===WORKFLOW.scheduled)return action('bulk-advance','批量推进工作流（已发布）',WORKFLOW.scheduled,'回填发布指标后确认已发布');
 return '<span class="form-help">选择「待制作 / 已制作 / 待排期 / 已排期」分类后可使用对应的批量工作流操作</span>';
}
// The next state of a category, taken from the shared pipeline definition.
function nextWorkflowState(state){const pipeline=[WORKFLOW.productionPending,WORKFLOW.productionComplete,WORKFLOW.schedulingPending,WORKFLOW.scheduled,WORKFLOW.published];const index=pipeline.indexOf(state);return index>=0&&index<pipeline.length-1?pipeline[index+1]:''}
// Fields collected by each workflow step. Unfilled fields are omitted so the
// backend keeps the existing value.
function workflowDialogFields(state){
 if(state===WORKFLOW.productionPending)return [
  {name:'materials',label:'素材说明',type:'text',placeholder:'可选：素材来源或制作说明'},
  {name:'tags',label:'Tag',type:'tags',placeholder:'多个 Tag 用逗号分隔（可选）'},
  {name:'notes',label:'备注',type:'textarea',placeholder:'可选：制作备注'}];
 if(state===WORKFLOW.productionComplete)return [
  {name:'owner_user_id',label:'内容负责人',type:'select',options:[{value:'',label:'保持不变'},...asArray(state.users).map(user=>({value:String(user.id),label:user.name}))]},
  {name:'performance_id',label:'关联演出',type:'select',options:[{value:'',label:'保持不变'},...asArray(state.performances).map(item=>({value:item.id,label:`${item.title} · ${String(item.starts_at||'').slice(0,10)}`}))]}];
 if(state===WORKFLOW.scheduled)return [
  {name:'views',label:'播放量',type:'number',placeholder:'可留空'},
  {name:'likes',label:'点赞量',type:'number',placeholder:'可留空'},
  {name:'comments',label:'评论量',type:'number',placeholder:'可留空'},
  {name:'saves',label:'收藏量',type:'number',placeholder:'可留空'}];
 return [];
}
// Reads the workflow dialog inputs. Empty inputs are dropped, so optional fields
// never overwrite existing data.
function readWorkflowFields(root,fields){
 const values={};
 for(const field of fields){
  const input=root.querySelector(`[data-workflow-field="${field.name}"]`);
  if(!input)continue;
  const raw=(input.value||'').trim();
  if(!raw)continue;
  if(field.type==='number'){const parsed=Number(raw);if(!Number.isFinite(parsed)||parsed<0){throw new Error(`${field.label}必须为不小于 0 的数字`)}values[field.name]=parsed}
  else if(field.name==='tags')values[field.name]=raw.split(/[,，]/).map(x=>x.trim()).filter(Boolean);
  else if(field.name==='owner_user_id')values[field.name]=Number(raw);
  else values[field.name]=raw;
 }
 return values;
}
// Single-record advance: same rule and field set as the batch flow, with the
// dialog collecting only the fields that step owns.
async function advanceSingleRecord(contentID){
 const record=state.records.find(item=>item.id===contentID);
 if(!record){toast('内容记录不存在');return}
 const next=nextWorkflowState(record.workflow_state);
 if(!next){toast('该内容已完成发布，没有可推进的状态');return}
 const fields=workflowDialogFields(record.workflow_state);
 await confirmDialog({
  title:`推进工作流（${next}）`,
  description:`将把「${record.title}」从「${record.workflow_state}」推进到「${next}」。未填写的可选字段保持原值。`,
  fields,
  confirmLabel:`推进到${next}`,
  onConfirm:async values=>{
   await api(`/api/contents/${contentID}/advance`,{method:'POST',body:JSON.stringify({fields:values})});
   await loadScheduleWindow();
   await render();
  },
 });
}
async function selectAllFilteredRecords(){state.selectedRecords=filteredRecords(state.records).map(x=>x.id);await renderRecordsOnly()}
function dropFromSelection(ids){const done=new Set(ids);state.selectedRecords=state.selectedRecords.filter(id=>!done.has(id))}
// Batch workflow advance for the selected category: one backend call carrying the
// step fields, per-record results from the server, refreshed list and statistics,
// and failed records kept selected for a retry.
async function bulkAdvanceWorkflow(){
 const category=state.recordsStatus,selected=new Set(state.selectedRecords);
 const records=state.records.filter(record=>selected.has(record.id)&&record.workflow_state===category);
 if(!records.length){toast(`当前选择中没有「${category}」内容`);return}
 const target=nextWorkflowState(category);
 if(!target){toast('该分类没有可推进的下一状态');return}
 const fields=workflowDialogFields(category);
 let outcome=null;
 const confirmed=await confirmDialog({
  title:`批量推进工作流（${target}）`,
  description:`将把已选择的 ${records.length} 条「${category}」内容推进到「${target}」。未填写的可选字段保持原值。`,
  details:records.slice(0,8).map(record=>record.title).concat(records.length>8?[`另有 ${records.length-8} 条记录`]:[]),
  fields,
  confirmLabel:'推进工作流',
  onConfirm:async(values)=>{
   const result=await api('/api/contents/advance-bulk',{method:'POST',body:JSON.stringify({target,items:records.map(record=>({id:record.id,fields:values}))})});
   const results=asArray(result&&result.results);
   dropFromSelection(results.filter(item=>item.status==='advanced').map(item=>item.id));
   outcome={advanced:num(result&&result.advanced),failed:results.filter(item=>item.status!=='advanced')};
   await render();
  },
 });
 if(!confirmed||!outcome)return;
 if(!outcome.failed.length){toast(`已推进 ${outcome.advanced} 条内容到${target}`);return}
 const lines=outcome.failed.slice(0,10).map(item=>`${escapeHtml(item.title||item.id)}：${escapeHtml(item.reason||'未能推进')}`).concat(outcome.failed.length>10?[`另有 ${outcome.failed.length-10} 条失败记录`]:[]);
 showInfo('批量推进结果',`成功 ${outcome.advanced} 条，失败 ${outcome.failed.length} 条。失败记录仍保持选中，可修正后重试。<br>${lines.join('<br>')}`);
}
// Batch manual scheduling: every record carries its own platform, date, time and
// note, so one record's plan is never applied to the others.
async function manualScheduleBatchDialog(){
 const policy=await ensureSchedulePolicy();
 const windows=asArray(policy&&policy.time_windows);
 const selected=new Set(state.selectedRecords);
 const records=state.records.filter(record=>selected.has(record.id)&&record.workflow_state===WORKFLOW.schedulingPending);
 if(!records.length){toast('当前选择中没有待排期内容');return}
 const defaultDate=pureDateKey(policy&&policy.window_start)||localDateKey(new Date());
 const defaultTime=windows[0]||'19:30';
 const platforms=platformChoices();
 const rowsFrom=values=>asArray(values.scheduleRows&&values.scheduleRows.length?values.scheduleRows:records.map(record=>({content_id:record.id,title:record.title,platform:record.platform||platforms[0],planned_date:defaultDate,planned_time:defaultTime,reason:''})));
 const body=values=>`<div class="schedule-batch"><table class="data-table"><thead><tr><th>内容</th><th>发布平台</th><th>计划日期</th><th>计划时间</th><th>备注</th></tr></thead><tbody>${rowsFrom(values).map((row,index)=>`<tr><td class="content-title-cell">${escapeHtml(row.title||'')}</td><td><select data-schedule-field="platform" data-schedule-index="${index}">${platforms.map(choice=>`<option ${choice===row.platform?'selected':''}>${escapeHtml(choice)}</option>`).join('')}</select></td><td><input type="date" data-schedule-field="planned_date" data-schedule-index="${index}" value="${escapeHtml(row.planned_date||'')}"/></td><td><input type="time" data-schedule-field="planned_time" data-schedule-index="${index}" value="${escapeHtml(row.planned_time||'')}"/></td><td><input data-schedule-field="reason" data-schedule-index="${index}" value="${escapeHtml(row.reason||'')}" placeholder="排期备注（可选）"/></td></tr>`).join('')}</tbody></table></div>`;
 const readValues=root=>{
  const items=records.map((record,index)=>{
   const value=field=>{const input=root.querySelector(`[data-schedule-field="${field}"][data-schedule-index="${index}"]`);return input?(input.value||'').trim():''};
   return {content_id:record.id,title:record.title,platform:value('platform'),planned_date:value('planned_date'),planned_time:value('planned_time'),owner:record.owner||'',reason:value('reason')};
  });
  const missing=items.find(item=>!item.platform||!item.planned_date||!item.planned_time);
  if(missing)throw new Error('每条记录都需要填写发布平台、计划日期和计划时间');
  return {scheduleRows:items};
 };
 await confirmDialog({
  title:'批量手动排期',
  description:`将为已选择的 ${records.length} 条待排期内容保存排期并推进到「已排期」。每条记录可以单独设置平台与时间。`,
  body,
  readValues,
  confirmLabel:'保存排期',
  onConfirm:async values=>{
   const outcome=await api('/api/schedule/manual-batch',{method:'POST',body:JSON.stringify({items:asArray(values.scheduleRows).map(row=>({content_id:row.content_id,platform:row.platform,planned_date:row.planned_date,planned_time:row.planned_time,owner:row.owner,reason:row.reason}))})});
   const results=asArray(outcome&&outcome.results);
   dropFromSelection(results.filter(item=>item.status==='scheduled').map(item=>item.content_id));
   await loadScheduleWindow();await render();
   const failed=results.filter(item=>item.status!=='scheduled');
   if(failed.length){
    showInfo('批量排期结果',`成功 ${num(outcome&&outcome.scheduled)} 条，失败 ${failed.length} 条。失败记录仍保持选中。<br>${failed.slice(0,10).map(item=>`${escapeHtml(item.title||item.content_id)}：${escapeHtml(item.reason||'未能排期')}`).join('<br>')}`);
    return;
   }
   toast(`已为 ${num(outcome&&outcome.scheduled)} 条内容保存排期`);
  },
 });
}
// Platform choices come from real team data plus the rule-set platform list.
function platformChoices(){
 const values=new Set();
 for(const record of state.records){if(record.platform)values.add(record.platform)}
 for(const row of asArray(state.scheduleWindowRows)){if(row.platform)values.add(row.platform)}
 for(const value of asArray(state.schedulePolicy&&state.schedulePolicy.platforms))values.add(value);
 if(!values.size)values.add('视频号');
 return [...values].sort();
}
async function settingsPage(){
 const s=state.settings,models=await api('/api/ai/configs');
 return `${heading('MODEL CONNECTION · 06','模型接口设置','仅管理模型供应商、Base URL、模型名称、API Key 与连接状态')}<section class="panel"><div class="panel-head"><div><h2 class="panel-title">模型接口</h2><div class="panel-subtitle">API Key 由后端保护并且不会回显；未配置模型时排期自动使用本地规则引擎</div></div><span class="connection-status"><i class="status-dot ${s.configured?'ok':''}"></i>${s.configured?'模型已配置':'当前使用规则引擎'}</span></div><div class="settings-note">${s.configured?'默认优先使用已配置模型。AI 单次分析最长等待 60 秒，失败时自动降级。':'可配置 OpenAI-compatible 服务商。默认模型名称为 qwen3.8-max，API Key 使用环境变量 AI_API_KEY 或安全配置流程提供。'}</div>${models.length>1?`<div class="model-profiles">${models.map(m=>`<div class="model-profile"><span>${escapeHtml(m.name)} · ${escapeHtml(m.model||'未填写')} ${m.enabled?'（当前启用）':''}</span>${m.enabled?'':`<button class="btn-secondary" data-action="activate-model" data-id="${m.id}">设为当前</button>`}</div>`).join('')}</div>`:''}<form id="aiForm" style="margin-top:16px"><div class="form-grid"><div class="form-field"><label>配置名称</label><input name="name" value="${escapeHtml(s.name||'默认模型')}"/></div><div class="form-field"><label>模型名称</label><input name="model" value="${escapeHtml(s.model||'qwen3.8-max')}"/></div><div class="form-field full"><label>Base URL</label><input name="base_url" type="url" value="${escapeHtml(s.base_url)}" placeholder="OpenAI-compatible API base URL"/></div><div class="form-field"><label>API Key ${s.has_key?`<small>（已保存 ${escapeHtml(s.key_hint)}；留空保留）</small>`:''}</label><input name="api_key" type="password" autocomplete="new-password" placeholder="${s.has_key?'留空保留原密钥':'AI_API_KEY 或输入密钥'}"/></div><input type="hidden" name="request_mode" value="base"/><input type="hidden" name="auth_type" value="bearer"/><input type="hidden" name="temperature" value="0.3"/><input type="hidden" name="fallback_enabled" value="true"/><input type="hidden" name="enabled" value="true"/></div><div class="form-actions"><button class="btn-secondary" type="button" data-action="test-ai">测试连接</button><button class="btn-primary">保存模型设置</button></div></form></section>`
}
const RULE_TYPE_LABELS={required:'必填校验',format:'格式校验（正则）',sensitive_words:'敏感词',length:'长度限制',tag_constraint:'标签约束'};
const RULE_TRIGGER_LABELS={'on_save':'内容保存时','content.save':'内容保存时','on_create':'内容创建时','content.create':'内容创建时','content.update':'内容更新时','on_update':'内容更新时',manual:'手动执行'};
function ruleTypeLabel(type){return RULE_TYPE_LABELS[type]||type||'未设置'}
function ruleTriggerLabel(trigger){return RULE_TRIGGER_LABELS[trigger]||trigger||'内容保存时'}
function ruleResultLabel(result){return result==='matched'?'命中':result==='no_match'?'未命中':result?'配置或执行错误':'未执行'}
function prettyRuleContent(value){if(value==null||value==='')return '{}';if(typeof value==='object')return JSON.stringify(value,null,2);try{return JSON.stringify(JSON.parse(String(value)),null,2)}catch{return String(value)}}
function ruleWarning(rule){const raw=String(rule.rule_content||'').trim();if(!raw)return '规则内容为空，内容保存时会报规则配置错误';let parsed;try{parsed=JSON.parse(raw)}catch{return '规则内容不是有效 JSON'};if(!parsed||typeof parsed!=='object'||Array.isArray(parsed))return '规则内容必须是 JSON 对象';if(rule.rule_type==='format'&&typeof parsed.pattern!=='string')return '格式校验缺少 pattern 字符串';if(rule.rule_type==='sensitive_words'&&!Array.isArray(parsed.words))return '敏感词规则缺少 words 数组';if(rule.rule_type==='length'&&parsed.min==null&&parsed.max==null)return '长度限制未设置 min 或 max';if(rule.rule_type==='tag_constraint'&&parsed.required==null&&parsed.max_count==null)return '标签约束未设置 required 或 max_count';return ''}
async function rulesPage(){
 const [teamResult,systemResult,logsResult]=await Promise.allSettled([api('/api/rules'),api('/api/rules/system'),api('/api/rules/logs?limit=10')]);
 const rules=teamResult.status==='fulfilled'?asArray(teamResult.value):[];
 const systemRules=systemResult.status==='fulfilled'?asArray(systemResult.value):[];
 const ruleLogs=logsResult.status==='fulfilled'?asArray(logsResult.value):[];
 state.rules=rules;state.systemRules=systemRules;state.ruleLogs=ruleLogs;
 const editor=['owner','editor'].includes(state.user?.role);
 const errorText=result=>result.status==='rejected'?escapeHtml(String(result.reason&&result.reason.message||result.reason||'请求失败')):'';
 const enabledCount=rules.filter(x=>x.enabled).length;
 const priorityCounts=rules.filter(x=>x.enabled).reduce((acc,x)=>{acc[x.priority]=(acc[x.priority]||0)+1;return acc},{});
 const conflicts=Object.keys(priorityCounts).filter(key=>priorityCounts[key]>1);
 const systemMarkup=systemResult.status==='rejected'?`<p class="hint error-text">系统规则集加载失败：${errorText(systemResult)}</p>`:systemRules.length?systemRules.map(x=>`<div class="rule-row"><div><b>${escapeHtml(x.name)}</b><small>版本 ${escapeHtml(x.version)} · 系统规则 ${num(x.rule_count)} 条 · 更新 ${escapeHtml(dateTimeText(x.updated_at))} · 来源：随服务发布的只读规则集</small><small>${escapeHtml(x.description||'无描述')}</small></div><span class="status-pill ${x.effective?'published':'unpublished'}">${x.effective?'当前生效（只读）':x.enabled?'已启用（只读）':'已停用（只读）'}</span></div>`).join(''):'<p class="hint">数据库中没有系统规则集记录；排期与内容校验将只依赖团队规则。</p>';
 const teamMarkup=teamResult.status==='rejected'?`<p class="hint error-text">团队规则加载失败：${errorText(teamResult)}</p>`:rules.length?rules.map(x=>{const warning=ruleWarning(x);return `<div class="rule-row"><div><b>${escapeHtml(x.name)}</b><small>优先级 ${num(x.priority)}（数值越小越先） · ${escapeHtml(ruleTypeLabel(x.rule_type))} · 范围 ${escapeHtml(x.content_type||'全部内容类型')} / ${escapeHtml(x.workflow_state||'全部工作流状态')} · 触发 ${escapeHtml(ruleTriggerLabel(x.trigger))}</small><small>规则内容 ${escapeHtml(x.rule_content||'{}')} · 最近执行 ${escapeHtml(ruleResultLabel(x.last_result))}${x.last_executed_at?`（${escapeHtml(dateTimeText(x.last_executed_at))}）`:''} · 更新 ${escapeHtml(dateTimeText(x.updated_at))} · 操作人 ${escapeHtml(x.updated_by||'未记录')}</small>${warning?`<small class="error-text">配置提示：${escapeHtml(warning)}</small>`:''}${x.last_detail?`<small>最近明细：${escapeHtml(x.last_detail)}</small>`:''}</div><span class="status-pill ${x.enabled?'published':'unpublished'}">${x.enabled?'启用':'停用'}</span>${editor?`<button class="btn-secondary" data-action="rule-edit" data-id="${x.id}">编辑</button><button class="btn-secondary" data-action="rule-toggle" data-id="${x.id}">${x.enabled?'停用':'启用'}</button><button class="btn-secondary danger-action" data-action="rule-delete" data-id="${x.id}">删除</button>`:''}</div>`}).join(''):'<p class="hint">当前团队尚未创建校验规则。团队规则为空时，内容保存不会因为校验规则被拒绝（复用系统规则集只约束排期周期与治理口径）。</p>';
 const editorForm=editor?`<form id="ruleForm" class="form-grid rule-editor"><input type="hidden" name="rule_id"/><div class="form-field"><label>规则名称</label><input name="name" required maxlength="120"/></div><div class="form-field"><label>规则类型</label><select name="rule_type"><option value="required">必填校验</option><option value="format">格式校验（正则）</option><option value="sensitive_words">敏感词</option><option value="length">长度限制</option><option value="tag_constraint">标签约束</option></select></div><div class="form-field"><label>内容类型范围</label><select name="content_type"><option value="">全部类型</option>${CATEGORIES.map(x=>`<option>${x}</option>`).join('')}</select></div><div class="form-field"><label>工作流状态范围</label><select name="workflow_state"><option value="">全部状态</option>${WORKFLOW_STATES.map(x=>`<option>${x}</option>`).join('')}</select></div><div class="form-field"><label>优先级（数值越小越先）</label><input type="number" name="priority" min="0" max="10000" value="100" required/></div><div class="form-field"><label>启用</label><select name="enabled"><option value="true">启用</option><option value="false">停用</option></select></div><div class="form-field full"><label>规则内容（JSON 对象）</label><textarea name="rule_content" rows="3">{}</textarea><small>示例：格式校验 {"pattern":"^[^#]+$"}；敏感词 {"words":["待核实"]}；长度限制 {"min":1,"max":120}；Tag {"required":[],"max_count":5}</small></div><div class="form-field full"><label>适用文本试运行</label><textarea id="rulePreviewText" rows="2" placeholder="输入标题或备注内容"></textarea><input id="rulePreviewTags" placeholder="试运行 Tag（逗号分隔）"/><div id="rulePreviewResult" class="form-help">试运行只校验当前编辑的这一条规则；命中表示内容保存时会因该规则被拒绝。</div></div><div class="form-field full"><label>备注</label><input name="notes" maxlength="500"/></div><div class="form-actions"><button type="button" class="btn-secondary" data-action="rule-preview">试运行</button><button type="button" class="btn-secondary" data-action="rule-clear">清空</button><button class="btn-primary">保存规则</button></div></form>`:'<p class="form-help">当前账号为只读角色：可以查看规则与执行日志，但不能创建、编辑或停用团队规则。</p>';
 const logMarkup=logsResult.status==='rejected'?`<p class="hint error-text">规则执行日志加载失败：${errorText(logsResult)}</p>`:ruleLogs.length?ruleLogs.map(x=>`<div class="rule-log-row"><span>${escapeHtml(dateTimeText(x.created_at))}</span><b>${escapeHtml(x.rule_name||'未命名规则')}</b><span class="rule-result ${escapeHtml(x.result)}">${escapeHtml(ruleResultLabel(x.result))}</span><small>${escapeHtml(x.detail)}</small></div>`).join(''):'<p class="hint">还没有规则执行记录：保存内容或试运行规则后会在这里显示。</p>';
 return `${heading('CONTENT POLICY · 08','内容规则集','系统规则集只读展示；团队校验规则按权限维护，并按优先级执行')}
 <section class="panel"><div class="panel-head"><div><h2 class="panel-title">系统规则集（只读）</h2><div class="panel-subtitle">随服务发布的版本化规则集：决定排期周期、允许时间窗与内容治理口径，应用内不提供编辑入口。</div></div></div>${systemMarkup}</section>
 <section class="panel" style="margin-top:15px"><div class="panel-head"><div><h2 class="panel-title">团队校验规则</h2><div class="panel-subtitle">共 ${rules.length} 条（启用 ${enabledCount} 条）· 按优先级从小到大执行，命中任一启用规则时内容保存会被拒绝；没有规则命中时保存继续。</div></div></div>${conflicts.length?`<p class="hint error-text">存在同优先级的启用规则（优先级 ${escapeHtml(conflicts.join('、'))}），保存新规则前请调整优先级。</p>`:''}${teamMarkup}${editorForm}<h3 class="panel-title rule-log-title">最近规则执行</h3><div class="rule-logs">${logMarkup}</div></section>`
}
async function tagsPage(){
 const query=(state.tagSearch||'').trim().toLocaleLowerCase();
 const rows=(state.tags||[]).filter(t=>t.name.toLocaleLowerCase().includes(query));
 const pageSize=20,pageCount=Math.max(1,Math.ceil(rows.length/pageSize));state.tagPage=Math.min(state.tagPage||1,pageCount);
 const shown=rows.slice((state.tagPage-1)*pageSize,state.tagPage*pageSize);
 return `${heading('TEAM TAG LIBRARY · 09','Tag 库','统一维护标签、查看使用情况；停用后历史内容仍保留')}
 <section class="panel"><div class="panel-head"><div><h2 class="panel-title">团队 Tag</h2><div class="panel-subtitle">忽略大小写、首尾空格和全半角差异；内容保存时可自动创建新 Tag。</div></div></div><div class="filter-grid"><label class="form-field"><span>搜索 Tag</span><input id="tagSearch" value="${escapeHtml(state.tagSearch||'')}" placeholder="输入名称"/></label></div><form id="tagCreateForm" class="inline-form"><input name="name" maxlength="50" required placeholder="新增 Tag 名称"/><button class="btn-primary">新增 Tag</button></form><div class="table-wrap"><table class="data-table"><thead><tr><th>名称</th><th>使用次数</th><th>创建时间</th><th>创建人 ID</th><th>状态</th><th>操作</th></tr></thead><tbody>${shown.map(t=>`<tr><td><input data-tag-name="${t.id}" value="${escapeHtml(t.name)}" ${!t.active?'disabled':''}/></td><td>${t.count}</td><td>${escapeHtml(t.created_at.slice(0,16).replace('T',' '))}</td><td>${escapeHtml(t.created_by||'未记录')}</td><td>${t.active?'启用':'停用'}</td><td><button class="btn-secondary" data-action="tag-save" data-id="${t.id}" ${!t.active?'disabled':''}>保存名称</button> <button class="btn-secondary" data-action="tag-toggle" data-id="${t.id}" data-active="${t.active}">${t.active?'停用':'启用'}</button> <button class="btn-secondary" data-action="tag-merge" data-id="${t.id}">合并</button></td></tr>`).join('')||'<tr><td colspan="6">没有匹配的 Tag</td></tr>'}</tbody></table></div><div class="pagination"><span>${rows.length} 个 Tag · 第 ${state.tagPage}/${pageCount} 页</span><button class="btn-secondary" data-action="tag-prev" ${state.tagPage<=1?'disabled':''}>上一页</button><button class="btn-secondary" data-action="tag-next" ${state.tagPage>=pageCount?'disabled':''}>下一页</button></div></section>`
}
async function teamPage(){
 const teams=await api('/api/teams'),users=await api('/api/users');state.users=users.map(u=>({...u,role_label:u.role==='owner'?'负责人':u.role==='editor'?'录入者':'查看者'}));const team=teams[0]||{};const owner=state.user?.role==='owner';
 return `${heading('ART TEAM WORKSPACE · 07','艺术团队管理','维护艺术团队信息、添加成员并管理团队权限')}<section class="panel"><div class="panel-head"><div><h2 class="panel-title">团队信息</h2><div class="panel-subtitle">${escapeHtml(team.name||'当前团队')} · 团队数据按成员权限隔离</div></div></div>${owner?`<form id="teamRenameForm" class="inline-form"><input name="name" required maxlength="100" value="${escapeHtml(team.name||'')}"/><button class="btn-primary">保存团队名称</button></form>`:'<p class="form-help">只有团队负责人可以修改团队信息。</p>'}</section><section class="panel" style="margin-top:15px"><div class="panel-head"><div><h2 class="panel-title">成员与权限</h2><div class="panel-subtitle">${users.length} 位成员 · 查看者只读</div></div></div><div class="team-members">${users.map(u=>`<div class="hypothesis-row"><div><b>${escapeHtml(u.name)} · ${escapeHtml(u.role_label)}</b><small>${escapeHtml(u.email)}</small></div></div>`).join('')||'<p class="hint">暂无成员</p>'}</div>${owner?`<form id="userForm" class="form-grid" style="margin-top:12px"><div class="form-field"><label>成员姓名</label><input name="name" required/></div><div class="form-field"><label>邮箱</label><input name="email" type="email" required/></div><div class="form-field"><label>初始密码（至少 10 位）</label><input name="password" type="password" minlength="10" required/></div><div class="form-field"><label>角色</label><select name="role"><option value="editor">录入者</option><option value="viewer">查看者</option></select></div><div class="form-actions"><button class="btn-primary">邀请成员</button></div></form>`:'<p class="form-help">只有团队负责人可以邀请或管理成员。</p>'}<p class="disclaimer">权限由服务端会话校验；客户端角色字段不授予写入权限。</p></section><section class="panel" style="margin-top:15px"><div class="panel-head"><h2 class="panel-title">操作审计</h2><button class="btn-secondary" data-action="audit">查看操作记录</button></div></section>`
}
async function runRules(id){const contentId=id||state.records[0]?.id;if(!contentId){toast('没有可用于规则执行的内容记录');return}try{const out=await api('/api/rules/evaluate',{method:'POST',body:JSON.stringify({content_id:contentId})});await render();toast(`手动规则已检查 ${out.checked} 条，命中 ${out.matched} 条`)}catch(e){toast(e.message)}}
function editBenchmark(b){showModal(`<h2>调整团队自身基准</h2><p>${escapeHtml(b.dataset.category)} · 手动基准会标注为团队调整值。</p><form id="benchmarkForm"><input type="hidden" name="category" value="${escapeHtml(b.dataset.category)}"/><div class="form-grid"><div class="form-field"><label>看完比例中位数（%）</label><input name="completion_median" type="number" min="0" max="100" step="0.1" required value="${escapeHtml(b.dataset.value||'0')}"/></div><div class="form-field"><label>互动比例中位数（%）</label><input name="interaction_median" type="number" min="0" step="0.1" required value="${escapeHtml(b.dataset.interaction||'0')}"/></div></div><div class="form-actions"><button type="button" class="btn-secondary" data-action="close">取消</button><button class="btn-primary">保存基准</button></div></form>`);qs('#benchmarkForm').onsubmit=async e=>{e.preventDefault();const d=Object.fromEntries(new FormData(e.currentTarget));d.completion_median=Number(d.completion_median);d.interaction_median=Number(d.interaction_median);try{await api('/api/benchmark',{method:'PUT',body:JSON.stringify(d)});closeModal();await render();toast('自身基准已更新')}catch(err){toast(err.message)}}}
function authPage(setup=false){const isSetup=setup===true,isRegister=setup==='register-team',needsFields=isSetup||isRegister;document.body.classList.add('auth-mode');qs('#sidebar').classList.add('auth-hidden');qs('#crumb').textContent=needsFields?'创建团队':'账号登录';qs('#pageRoot').innerHTML=`<div class="auth-shell"><div class="eyebrow">ARTSMART STRATEGY</div><h1>${needsFields?'创建团队管理员':'登录艺数智策'}</h1><p>${needsFields?'设置团队与负责人账号，建立独立的私有数据空间。':'使用团队负责人提供的邮箱与密码登录。'}</p><form id="authForm" class="auth-form">${needsFields?`<label>团队名称<input name="team_name" required value="示例艺术团队"/></label><label>你的姓名<input name="name" required/></label>`:''}<label>邮箱<input name="email" type="email" required autocomplete="username"/></label><label>密码<input name="password" type="password" required minlength="10" autocomplete="${needsFields?'new-password':'current-password'}"/></label><div id="authError" class="auth-error"></div><button class="btn-primary">${needsFields?'创建账号并进入':'登录'}</button></form>${needsFields?'':'<button id="createTeamAccount" class="btn-secondary" style="margin-top:10px">创建新的团队空间</button>'}<p class="disclaimer">你的记录仅在当前团队空间内可见。建议部署在本机或团队内网。</p></div>`;qs('#authForm').onsubmit=async e=>{e.preventDefault();const d=Object.fromEntries(new FormData(e.currentTarget));try{const path=isSetup?'/api/auth/setup':isRegister?'/api/auth/register-team':'/api/auth/login';const res=await api(path,{method:'POST',body:JSON.stringify(d)});sessionStorage.setItem('artsSessionToken',res.access_token);state.user=res.user;state.teamId=res.user.team_id;localStorage.setItem('artsTeamId',state.teamId);document.body.classList.remove('auth-mode');qs('#sidebar').classList.remove('auth-hidden');syncPageChrome();await render();setIdentity();toast('登录成功')}catch(err){const message=qs('#authError');if(message)message.textContent=err.message;else toast(err.message)}};const create=qs('#createTeamAccount');if(create)create.onclick=()=>authPage('register-team')}
function setIdentity(){if(!state.user)return;qs('#teamName').textContent=state.user.team_name;qs('#userName').textContent=state.user.name;qs('#userRole').textContent=state.user.role==='owner'?'团队负责人':state.user.role==='editor'?'内容录入者':'只读查看者'}
async function init(){try{state.runtimeMode=await api('/api/runtime-mode');const banner=qs('#persistenceBanner');if(banner&&state.runtimeMode.mode==='example'){banner.hidden=false;banner.textContent=state.runtimeMode.message+' 当前修改仅供本次演示使用。'}}catch(err){console.error('运行模式检测失败',err)}qs('#todayLabel').textContent=new Intl.DateTimeFormat('zh-CN',{year:'numeric',month:'long',day:'numeric',weekday:'short'}).format(new Date());qs('#menuToggle').onclick=()=>qs('#sidebar').classList.toggle('open');qs('#resetData').onclick=async()=>{try{await api('/api/auth/logout',{method:'POST'})}catch{}sessionStorage.removeItem('artsSessionToken');state.user=null;authPage(false)};try{const status=await api('/api/auth/status');if(status.setup_required){authPage(true);return}const token=sessionStorage.getItem('artsSessionToken');if(!token){authPage(false);return}state.user=await api('/api/auth/me');state.teamId=state.user.team_id;localStorage.setItem('artsTeamId',state.teamId);setIdentity();qs('#sidebar').classList.remove('auth-hidden');syncPageChrome();await render()}catch(e){authPage(false)}}
init();
