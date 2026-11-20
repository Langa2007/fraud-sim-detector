#!/usr/bin/env python3
"""Replay JSONL assessment events and report rule catch/friction by outcome."""
import argparse, json, urllib.request, urllib.error, time

def main():
    p=argparse.ArgumentParser(); p.add_argument("events"); p.add_argument("--url",default="http://localhost:8080/v1/assess"); p.add_argument("--tenant",default="demo"); p.add_argument("--key",default="demo-key"); p.add_argument("--secret",default="demo-secret"); a=p.parse_args()
    import hashlib,hmac
    tp=fp=fn=tn=errors=0; lat=[]
    with open(a.events,encoding="utf-8") as f:
        for line in f:
            row=json.loads(line); label=bool(row.pop("confirmed_fraud",False)); body=json.dumps(row,separators=(",",":"),ensure_ascii=False).encode(); ts=str(int(time.time())); sig=hmac.new(a.secret.encode(),ts.encode()+b"."+body,hashlib.sha256).hexdigest()
            req=urllib.request.Request(a.url,data=body,headers={"Content-Type":"application/json","X-Kinga-Key":a.key,"X-Kinga-Timestamp":ts,"X-Kinga-Signature":sig,"Idempotency-Key":row["tenant_event_id"]})
            start=time.perf_counter()
            try:
                with urllib.request.urlopen(req,timeout=5) as res: result=json.load(res)
                lat.append((time.perf_counter()-start)*1000); flagged=result["decision"]!="ALLOW"
                if label and flagged: tp+=1
                elif label: fn+=1
                elif flagged: fp+=1
                else: tn+=1
            except Exception as ex: errors+=1; print(f"error {row.get('tenant_event_id')}: {ex}")
    n=tp+fp+fn+tn; print(json.dumps({"events":n+errors,"errors":errors,"recall":tp/(tp+fn) if tp+fn else None,"false_positive_rate":fp/(fp+tn) if fp+tn else None,"precision":tp/(tp+fp) if tp+fp else None,"latency_ms":{"p50":pct(lat,.5),"p95":pct(lat,.95),"p99":pct(lat,.99)},"confusion":{"tp":tp,"fp":fp,"fn":fn,"tn":tn}},indent=2))
def pct(xs,q):
    if not xs:return None
    xs=sorted(xs);return round(xs[min(len(xs)-1,int((len(xs)-1)*q))],2)
if __name__=="__main__":main()
