# run from anywhere: NVIDIA_API_KEY=... python3 eval/score.py [runs per photo]
import base64, concurrent.futures as cf, json, os, re, subprocess, sys, tempfile, time, urllib.error, urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
VISION = os.path.join(HERE, "..", "agent", "vision.go")
MODEL = os.environ.get("NEMOTRON_MODEL", "nvidia/nemotron-3.5-super-vl-120b-a12b")
KEY = os.environ["NVIDIA_API_KEY"]
RUNS = int(sys.argv[1]) if len(sys.argv) > 1 else 3

prompt = re.search(r"const visionPrompt = `(.*?)`", open(VISION).read(), re.S).group(1)
golden = json.load(open(os.path.join(HERE, "golden.json")))
PHOTOS = os.path.join(HERE, "photos")

def shrink(path):
    # same size as the messenger sends
    out = tempfile.NamedTemporaryFile(suffix=".jpg", delete=False).name
    subprocess.run(["sips", "-Z", os.environ.get("SIDE", "1536"), "-s", "format", "jpeg", path, "--out", out], capture_output=True, check=True)
    return base64.b64encode(open(out, "rb").read()).decode()

def look(b64):
    body = {"model": MODEL, "max_tokens": 800, "temperature": 0.2,
            "chat_template_kwargs": {"enable_thinking": False},
            "messages": [{"role": "user", "content": [
                {"type": "text", "text": prompt},
                {"type": "image_url", "image_url": {"url": "data:image/jpeg;base64," + b64}}]}]}
    req = urllib.request.Request("https://integrate.api.nvidia.com/v1/chat/completions", data=json.dumps(body).encode(),
                                 headers={"Authorization": "Bearer " + KEY, "Content-Type": "application/json"})
    for attempt in range(5):
        t = time.time()
        try:
            r = json.load(urllib.request.urlopen(req, timeout=120))
        except urllib.error.HTTPError as e:
            if e.code == 429:
                time.sleep(3 * 2 ** attempt)
                continue
            raise
        text = r["choices"][0]["message"]["content"].strip()
        text = re.sub(r"^```(json)?|```$", "", text.strip()).strip()
        return json.loads(text), time.time() - t
    raise RuntimeError("rate limited")

def has(value, pattern):
    return value is not None and re.search(pattern, str(value).lower()) is not None

def score(case, got):
    problems = []
    name = " ".join(str(got.get(k) or "") for k in ("brand", "product", "model", "variant")).lower()
    # with other products listed, echo asks which one, same as not confident
    sure = got.get("confident") is True and not got.get("other_products")
    if "confident" in case and sure != case["confident"]:
        problems.append(f"confident {got.get('confident')}")
    if case.get("brand") and not has(name, case["brand"]):
        problems.append(f"brand {got.get('brand')!r}")
    for w in case.get("words", []):
        if w not in name:
            problems.append(f"missing {w!r}")
    if case.get("model") and got.get("model") not in (None, case["model"]):
        problems.append(f"made up model {got.get('model')!r}")
    if "price" in case:
        if case["price"] is None and got.get("price") is not None:
            problems.append(f"made up price {got.get('price')}")
        elif case["price"] is not None and (got.get("price") is None or abs(float(got["price"]) - case["price"]) > 0.01):
            problems.append(f"price {got.get('price')}")
    if "old_price" in case and (got.get("claimed_old_price") is None or abs(float(got["claimed_old_price"]) - case["old_price"]) > 0.01):
        problems.append(f"old price {got.get('claimed_old_price')}")
    # the worst outcome: sure of itself and wrong
    wrong_and_sure = bool(problems) and sure
    return problems, wrong_and_sure

def run(case):
    b64 = shrink(os.path.join(PHOTOS, case["file"]))
    out = []
    for _ in range(RUNS):
        try:
            got, secs = look(b64)
            problems, sure = score(case, got)
        except Exception as e:
            got, secs, problems, sure = {}, 0, [f"error {e}"], False
        out.append({"got": got, "secs": secs, "problems": problems, "wrong_and_sure": sure})
    return case, out

with cf.ThreadPoolExecutor(4) as pool:
    results = list(pool.map(run, golden))

total = right = sure_wrong = 0
kinds = {}
secs = []
for case, runs in results:
    ok = sum(1 for r in runs if not r["problems"])
    total += len(runs)
    right += ok
    sure_wrong += sum(1 for r in runs if r["wrong_and_sure"])
    k = kinds.setdefault(case["kind"], [0, 0])
    k[0] += ok
    k[1] += len(runs)
    secs += [r["secs"] for r in runs if r["secs"]]
    mark = "OK  " if ok == len(runs) else f"{ok}/{len(runs)} "
    print(f"{mark} {case['file']:<24}", "; ".join(sorted({p for r in runs for p in r["problems"]})))
    first = runs[0]["got"]
    print(f"      {first.get('brand')} | {first.get('product')} | {first.get('model')} | {first.get('variant')} | "
          f"price {first.get('price')} old {first.get('claimed_old_price')} | confident {first.get('confident')} | others {first.get('other_products')}")

print(f"\n{right}/{total} right, {sure_wrong} wrong while confident, {sum(secs)/max(len(secs),1):.1f}s avg")
for kind, (ok, n) in kinds.items():
    print(f"  {kind:<17} {ok}/{n}")
