#!/usr/bin/env python3
"""Replay canonical assessment JSONL and report catch/friction by outcome."""
import argparse, json, urllib.request, urllib.error, time


def load_labels(path):
    if not path:
        return {}
    labels = {}
    with open(path, encoding="utf-8") as source:
        for line_number, line in enumerate(source, 1):
            if not line.strip():
                continue
            row = json.loads(line)
            event_id = row.get("tenant_event_id")
            if not event_id or event_id in labels or not isinstance(row.get("confirmed_fraud"), bool):
                raise ValueError(f"{path}:{line_number}: missing/duplicate event ID or invalid fraud label")
            labels[event_id] = row
    return labels

def main():
    p=argparse.ArgumentParser(); p.add_argument("events"); p.add_argument("--labels",help="optional synthetic label sidecar JSONL"); p.add_argument("--url",default="http://localhost:8080/v1/assess"); p.add_argument("--tenant",default="demo"); p.add_argument("--key",default="demo-key"); p.add_argument("--secret",default="demo-secret"); a=p.parse_args()
    import hashlib,hmac
    tp=fp=fn=tn=errors=0; lat=[]; by_typology={}; labels=load_labels(a.labels); seen=set()
    with open(a.events,encoding="utf-8") as f:
        for line in f:
            row=json.loads(line); event_id=row.get("tenant_event_id")
            if not event_id or event_id in seen:
                raise ValueError(f"missing or duplicate tenant_event_id: {event_id!r}")
            seen.add(event_id)
            if a.labels and event_id not in labels:
                raise ValueError(f"{a.labels}: label missing for event {event_id}")
            sidecar=labels.get(event_id,{}); label=sidecar.get("confirmed_fraud",bool(row.pop("confirmed_fraud",False))); typology=sidecar.get("typology", "fraud" if label else "legitimate"); stats=by_typology.setdefault(typology,{"tp":0,"fp":0,"fn":0,"tn":0}); body=json.dumps(row,separators=(",",":"),ensure_ascii=False).encode(); ts=str(int(time.time())); sig=hmac.new(a.secret.encode(),ts.encode()+b"."+body,hashlib.sha256).hexdigest()
            req=urllib.request.Request(a.url,data=body,headers={"Content-Type":"application/json","X-Kinga-Key":a.key,"X-Kinga-Timestamp":ts,"X-Kinga-Signature":sig,"Idempotency-Key":row["tenant_event_id"]})
            start=time.perf_counter()
            try:
                with urllib.request.urlopen(req,timeout=5) as res: result=json.load(res)
                lat.append((time.perf_counter()-start)*1000); flagged=result["decision"]!="ALLOW"
                if label and flagged: tp+=1; stats["tp"]+=1
                elif label: fn+=1; stats["fn"]+=1
                elif flagged: fp+=1; stats["fp"]+=1
                else: tn+=1; stats["tn"]+=1
            except Exception as ex: errors+=1; print(f"error {row.get('tenant_event_id')}: {ex}")
    n=tp+fp+fn+tn
    for stats in by_typology.values():
        stats["recall"] = stats["tp"]/(stats["tp"]+stats["fn"]) if stats["tp"]+stats["fn"] else None
        stats["false_positive_rate"] = stats["fp"]/(stats["fp"]+stats["tn"]) if stats["fp"]+stats["tn"] else None
    print(json.dumps({"events":n+errors,"errors":errors,"recall":tp/(tp+fn) if tp+fn else None,"false_positive_rate":fp/(fp+tn) if fp+tn else None,"precision":tp/(tp+fp) if tp+fp else None,"latency_ms":{"p50":pct(lat,.5),"p95":pct(lat,.95),"p99":pct(lat,.99)},"confusion":{"tp":tp,"fp":fp,"fn":fn,"tn":tn},"typologies":by_typology},indent=2))
def pct(xs,q):
    if not xs:return None
    xs=sorted(xs);return round(xs[min(len(xs)-1,int((len(xs)-1)*q))],2)
if __name__=="__main__":main()
