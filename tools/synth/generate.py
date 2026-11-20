#!/usr/bin/env python3
"""Deterministic synthetic Kenyan mobile-money assessment events (no real PII)."""
import argparse,json,random,datetime
def main():
 p=argparse.ArgumentParser();p.add_argument("--count",type=int,default=1000);p.add_argument("--seed",type=int,default=7);p.add_argument("--out",default="synthetic.jsonl");a=p.parse_args();r=random.Random(a.seed);now=datetime.datetime.now(datetime.timezone(datetime.timedelta(hours=3)))
 with open(a.out,"w",encoding="utf-8") as f:
  for i in range(a.count):
   fraud=r.random()<.06;dt=now-datetime.timedelta(seconds=r.randint(0,60*86400));ev={"tenant_event_id":f"synth-{i:08}","event_type":"funds_transfer","occurred_at":dt.isoformat(),"channel":r.choice(["mobile_app","ussd","agent"]),"subject":{"msisdn":f"+2547{r.randint(10000000,99999999)}","account_ref":f"acct-{i:07}"},"transaction":{"amount":round(r.uniform(200,250000),2),"currency":"KES","rail":r.choice(["mpesa","pesalink","bank_transfer"]),"beneficiary":{"type":"wallet","ref":f"ben-{r.randint(1,3000)}"}},"signals":{}}
   if fraud:ev["signals"]={"sim_swap_age_minutes":r.randint(1,120),"device_first_seen":True,"beneficiary_first_seen":True,"balance_drain_ratio":round(r.uniform(.7,.99),2),"transfers_in_window":r.randint(3,6),"new_beneficiaries_in_window":r.randint(2,4),"window_minutes":r.randint(4,15)}
   ev["confirmed_fraud"]=fraud;f.write(json.dumps(ev,separators=(",",":"))+"\n")
 print(f"wrote {a.count} synthetic events to {a.out} (seed={a.seed})")
if __name__=="__main__":main()
