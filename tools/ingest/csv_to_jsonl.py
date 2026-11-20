#!/usr/bin/env python3
"""Convert canonical Phase 1 CSV columns into assessment JSONL for replay."""
import argparse, csv, json, os, sys, tempfile

def val(row, key): return (row.get(key) or "").strip()
def number(row,key,cast):
    x=val(row,key)
    return cast(x) if x else None
def make(row):
    e={"tenant_event_id":val(row,"tenant_event_id"),"event_type":val(row,"event_type"),"occurred_at":val(row,"occurred_at"),"channel":val(row,"channel"),"subject":{}}
    for src,dst in (("msisdn","msisdn"),("account_ref","account_ref"),("customer_ref","customer_ref")):
        if val(row,src):e["subject"][dst]=val(row,src)
    device={k:val(row,k) for k in ("device_id","ip","app_version") if val(row,k)}
    if device:e["device"]=device
    tx={}; amount=number(row,"amount",float)
    if amount is not None:tx["amount"]=amount
    for k in ("currency","rail"):
        if val(row,k):tx[k]=val(row,k)
    beneficiary={k:val(row,"beneficiary_"+k) for k in ("type","ref") if val(row,"beneficiary_"+k)}
    if beneficiary:tx["beneficiary"]=beneficiary
    if tx:e["transaction"]=tx
    signals={}
    for key in ("sim_swap_age_minutes","transfers_in_window","new_beneficiaries_in_window","window_minutes","account_age_days","inbound_fan_in"):
        n=number(row,key,int)
        if n is not None:signals[key]=n
    for key in ("balance_drain_ratio",):
        n=number(row,key,float)
        if n is not None:signals[key]=n
    for key in ("device_first_seen","beneficiary_first_seen","rapid_outbound"):
        x=val(row,key).lower()
        if x:
            if x not in ("true","false","1","0","yes","no"):raise ValueError(f"{key} must be a boolean")
            signals[key]=x in ("true","1","yes")
    if signals:e["signals"]=signals
    if val(row,"confirmed_fraud").lower() in ("true","1","yes"):e["confirmed_fraud"]=True
    elif val(row,"confirmed_fraud").lower() in ("false","0","no"):e["confirmed_fraud"]=False
    elif val(row,"confirmed_fraud"):raise ValueError("confirmed_fraud must be a boolean")
    return e
def main():
    p=argparse.ArgumentParser();p.add_argument("source");p.add_argument("destination");a=p.parse_args()
    rows=[]
    if a.source.lower().endswith((".parquet",".pq")):
        try: import pyarrow.parquet as pq
        except ImportError: print("Parquet input requires pyarrow (python -m pip install pyarrow)",file=sys.stderr);return 2
        rows=pq.read_table(a.source).to_pylist()
    else:
        with open(a.source,newline="",encoding="utf-8-sig") as f: rows=list(csv.DictReader(f))
    directory=os.path.dirname(os.path.abspath(a.destination))
    fd,tmp=tempfile.mkstemp(prefix=".kinga-ingest-",dir=directory,text=True)
    try:
        with os.fdopen(fd,"w",encoding="utf-8") as out:
            for i,row in enumerate(rows,2):
                try: event=make(row)
                except Exception as ex: raise ValueError(f"row {i}: {ex}") from ex
                out.write(json.dumps(event,separators=(",",":"),ensure_ascii=False)+"\n")
        os.replace(tmp,a.destination)
    except Exception:
        try:os.unlink(tmp)
        except FileNotFoundError:pass
        raise
    print(f"converted {len(rows)} rows to {a.destination}")
if __name__=="__main__":
    try:main()
    except Exception as e:print(e,file=sys.stderr);sys.exit(1)
