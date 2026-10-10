"""End-to-end smoke test against an isolated temporary SQLite database."""
from __future__ import annotations

import json
import os
import sys
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

tmp = tempfile.TemporaryDirectory(prefix="arts-smart-")
os.environ["DATABASE_URL"] = f"sqlite:///{Path(tmp.name) / 'selftest.sqlite'}"
os.environ["SEED_ON_START"] = "true"
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from fastapi.testclient import TestClient
from backend.main import app


class FakeModel(BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get("Content-Length", "0")); self.rfile.read(n)
        body = {"choices": [{"message": {"content": json.dumps({"recommendations": [{
            "content_type": "艺术科普", "platform": "内容社区C", "day_of_week": "周四",
            "time_slot": "19:30", "title": "从一次练习开始讲起", "reason": "这组建议来自模拟模型自测。",
            "reference_sample_count": 3, "confidence": "low"}]}, ensure_ascii=False)}}]}
        raw=json.dumps(body,ensure_ascii=False).encode()
        self.send_response(200);self.send_header("Content-Type","application/json");self.send_header("Content-Length",str(len(raw)));self.end_headers();self.wfile.write(raw)
    def log_message(self,*_): pass


def run():
    fake=ThreadingHTTPServer(("127.0.0.1",0),FakeModel);threading.Thread(target=fake.serve_forever,daemon=True).start()
    base=f"http://127.0.0.1:{fake.server_port}/v1"
    with TestClient(app) as client:
        checks=[]
        def check(name, condition):
            if not condition: raise AssertionError(name)
            checks.append(name);print(f"PASS {name}")
        check("protected API requires login",client.get('/api/contents').status_code==401)
        setup=client.post('/api/auth/setup',json={'email':'owner@example.test','password':'selftest-password','name':'测试负责人','team_name':'自测团队'})
        check("first-run administrator setup",setup.status_code==200)
        client.headers.update({'Authorization':'Bearer '+setup.json()['access_token'],'X-Team-Id':'1'})
        check("health and database",client.get('/api/health').status_code==200)
        initial=client.get('/api/contents').json();check("seed records loaded",initial['count']==3)
        check("small sample suppresses category mean",client.get('/api/stats/category').json()['items'][0]['views'] is None)
        rule=client.post('/api/ai/recommend').json();check("model-free recommendation fallback",rule['source']=='rule' and '规则分析' in rule['source_label'] and len(rule['recommendations'])==3)
        record={"title":"排练细节｜动作拆解练习","category":"技艺展示","platform":"短视频平台B","published_at":"2026-10-06T19:30:00","duration":45,"views":2100,"likes":180,"comments":20,"saves":35,"shares":12,"completion":71,"tags":["排练","技巧"],"lifecycle":"已发布","profile_visits":30,"follows":4,"attendance":2,"tickets":1,"comment_text":"动作清楚，喜欢这个细节"}
        created=client.post('/api/contents',json=record);check("create a content asset",created.status_code==200 and created.json()['item']['title']==record['title'])
        cid=created.json()['item']['id'];check("content appears in live overview",client.get('/api/stats/overview').json()['total_posts']==4)
        check("tag dictionary receives content tags",any(x['name']=='排练' for x in client.get('/api/tags').json()))
        csv='平台,发布时间,标题,播放量,点赞量,看完比例\n短视频平台A,2026-10-07 18:00,"CSV 导入, 标题",1250,90,65\n'
        imp=client.post('/api/contents/import-csv',files={'file':('test.csv',csv.encode(),'text/csv')});check("CSV import and quoted title",imp.status_code==200 and imp.json()['imported']==1 and 'CSV 导入, 标题' in imp.json()['items'][0]['title'])
        check("minimum sample setting",client.post('/api/settings/sample-threshold',json={'min_sample':2}).json()['min_sample']==2)
        check("heatmap endpoint",len(client.get('/api/stats/heatmap').json()['items'])==35)
        other=client.post('/api/auth/register-team',json={'email':'other@example.test','password':'selftest-password','name':'另一团队','team_name':'另一支体验团队'})
        check("create isolated team account",other.status_code==200)
        check("team isolation",client.get('/api/contents',headers={'X-Team-Id':str(other.json()['user']['team_id'])}).status_code==403)
        viewer=client.post('/api/users',json={'email':'viewer@example.test','password':'selftest-password','name':'只读成员','role':'viewer'});check("create viewer account",viewer.status_code==200)
        viewer_login=client.post('/api/auth/login',json={'email':'viewer@example.test','password':'selftest-password'}).json()
        check("viewer role is read-only",client.post('/api/contents',headers={'Authorization':'Bearer '+viewer_login['access_token'],'X-Team-Id':'1'},json=record).status_code==403)
        # Exercise actual server-proxied model generation against a local mock server.
        config=client.get('/api/settings').json()
        config.update({'base_url':base,'model':'selftest-model','api_key':'selftest-key','request_mode':'base','fallback_enabled':True})
        saved=client.put('/api/settings',json=config);check("persist model proxy settings",saved.status_code==200 and saved.json()['configured'])
        live=client.post('/api/ai/recommend').json();check("AI recommendation proxy and JSON parsing",live['source']=='selftest-model' and live['recommendations'][0]['title']=='从一次练习开始讲起')
        adopted=client.post('/api/schedule/adopt',json={'recommendation_id':live['id'],'items':live['recommendations']})
        check("adopt recommendation into persistent schedule",adopted.status_code==200 and adopted.json()['created']==1)
        sched=client.get('/api/schedule').json()[0];check("schedule read after adoption",sched['title']=='从一次练习开始讲起')
        patched=client.patch(f"/api/schedule/{sched['id']}",json={'planned_time':'20:00','title':'调整后的建议标题'});check("edit persisted schedule",patched.status_code==200 and patched.json()['planned_time']=='20:00')
        feedback={"title":"调整后的建议标题","category":"艺术科普","platform":"内容社区C","published_at":"2026-10-10T20:00:00","views":1800,"likes":140,"comments":15,"saves":24,"shares":10,"completion":72,"profile_visits":60,"follows":6,"attendance":4,"tickets":3,"comment_text":"讲得清楚"}
        result=client.post(f"/api/schedule/{sched['id']}/publish",json=feedback);check("publish feedback closes loop",result.status_code==200 and result.json()['content']['adopted'] is True)
        check("adoption comparison endpoint",client.get('/api/stats/adoption').json()['adopted']['sample_count']==1)
        pdf=client.get('/api/report/export');check("PDF report export",pdf.status_code==200 and pdf.content.startswith(b'%PDF') and len(pdf.content)>1000)
        hand=client.get('/api/handover/export?format=markdown');check("handover pack export",hand.status_code==200 and '运营交接包' in hand.text)
        anon=client.get('/api/export/anonymized');check("anonymized CSV export",anon.status_code==200 and '标题' not in anon.text)
        check("audit log records operations",len(client.get('/api/audit').json())>=4)
        print(f"\n{len(checks)} end-to-end checks passed.")
    fake.shutdown();tmp.cleanup()


if __name__=='__main__':run()
