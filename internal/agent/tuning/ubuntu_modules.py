"""Fixed Ubuntu BBR/fq package operation. Invoked inside the packaged oneshot."""
import hashlib
from contextlib import contextmanager
import os
from pathlib import Path
import platform
import posixpath
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile


TARGETS = {"bbr": "net/ipv4/tcp_bbr", "fq": "net/sched/sch_fq"}
KEYRING = "/usr/share/keyrings/ubuntu-archive-keyring.gpg"


def build_version(release, signature):
    match = re.fullmatch(r"(\d+\.\d+\.\d+-\d+)-([a-z0-9][a-z0-9-]*)", release)
    fields = signature.split()
    if not match or len(fields) != 3 or fields[0] != "Ubuntu":
        raise ValueError(f"无法确认 Ubuntu 运行内核构建：{release} {signature}")
    build = re.fullmatch(re.escape(match[1]) + r"(\.[0-9]+(?:[+~.][a-zA-Z0-9.+~]+)?)" + "-" + re.escape(match[2]), fields[1])
    if not build:
        raise ValueError(f"运行内核与 version_signature 不匹配：{release} {signature}")
    return match[1] + build[1]


def inspect_payload(entries, release):
    found = set()
    tree = "lib/modules/" + release + "/"
    for entry in entries:
        path = entry.name.removeprefix("./").rstrip("/")
        if path.startswith("/") or ".." in path.split("/"):
            raise ValueError("归档路径不安全：" + path)
        if entry.isdir():
            continue
        path = path.removeprefix("usr/") if path.startswith("usr/lib/") else path
        if path.startswith(tree):
            if posixpath.basename(path).startswith(("vmlinuz", "vmlinux", "bzImage", "Image", "initrd.img")):
                raise ValueError("归档混入内核镜像：" + path)
            if entry.issym() and not posixpath.normpath(posixpath.join(posixpath.dirname(path), entry.linkname)).startswith(tree):
                raise ValueError("模块链接越界：" + path)
            if not (entry.issym() or entry.isfile()):
                raise ValueError("未知模块文件类型：" + path)
            for target, subpath in TARGETS.items():
                if entry.isfile() and re.fullmatch(re.escape(tree + "kernel/" + subpath) + r"\.ko(?:\.(?:xz|gz|zst))?", path):
                    found.add(target)
        elif entry.isfile() and (path in {"boot/config-" + release, "boot/System.map-" + release,
                                         "lib/modprobe.d/blacklist_linux_" + release + ".conf"}
                                  or any(path.startswith("usr/share/doc/linux-" + prefix + release + "/")
                                         for prefix in ("modules-", "modules-extra-"))):
            continue
        else:
            raise ValueError("归档包含独立模块包以外的文件：" + path)
    return found


def check_plan(plan, name):
    actions = [line.split()[:2] for line in plan.splitlines() if line.startswith(("Inst ", "Conf ", "Remv ", "Purg "))]
    if actions != [["Inst", name], ["Conf", name]]:
        raise ValueError(f"依赖预检要求变动其他包或无法单独配置 {name}：{actions}")


def run(args, **kwargs):
    proc = subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                          stdin=subprocess.DEVNULL, **kwargs)
    if proc.returncode:
        print(proc.stdout, proc.stderr, file=sys.stderr)
        detail = (proc.stderr.strip() or proc.stdout.strip() or str(proc.returncode)).splitlines()[-1]
        raise RuntimeError(f"{args[0]} {args[1]}：{detail}")
    return proc.stdout.strip()


def host_identity():
    if sys.version_info < (3, 9):
        raise ValueError("需要已有的系统 Python 3.9 或更高版本")
    release = platform.release()
    distribution = dict(line.split("=", 1) for line in Path("/etc/os-release").read_text().splitlines() if "=" in line)
    codename = distribution.get("VERSION_CODENAME", "").strip('"')
    if distribution.get("ID", "").strip('"') != "ubuntu" or not re.fullmatch("[a-z]+", codename):
        raise ValueError("仅支持 Ubuntu 官方独立模块包，请在宿主处理")
    detector = subprocess.run(["/usr/bin/systemd-detect-virt", "--container"], capture_output=True, text=True)
    if detector.returncode != 1 or detector.stdout.strip() != "none":
        raise ValueError("宿主内核归属无法确认：" + (detector.stdout + detector.stderr).strip())
    if Path("/proc/self/uid_map").read_text().split() != ["0", "0", "4294967295"] or any(
            Path(p).exists() for p in ("/.dockerenv", "/run/.containerenv", "/proc/vz", "/run/systemd/container")):
        raise ValueError("容器或用户命名空间共享宿主内核，请在宿主处理")
    version = build_version(release, Path("/proc/version_signature").read_text().strip())
    arch = run(["/usr/bin/dpkg", "--print-architecture"])
    if {"x86_64": "amd64", "aarch64": "arm64"}.get(platform.machine()) != arch:
        raise ValueError("运行架构与受支持的 Ubuntu 包架构不符：" + arch)
    fmt = "${db:Status-Status} ${Version} ${Architecture}"
    image = "linux-image-" + release
    if run(["/usr/bin/dpkg-query", "-W", "-f=" + fmt, image]) != f"installed {version} {arch}":
        raise ValueError(f"已装镜像与运行构建不匹配：{image} {version} {arch}")
    if not Path("/usr/lib/modules").is_dir():
        raise FileNotFoundError("/usr/lib/modules")
    for p in [KEYRING, "/usr/bin/apt-get", "/usr/bin/apt-cache", "/usr/bin/dpkg-deb"]:
        if not Path(p).is_file():
            raise FileNotFoundError(p)
    return release, version, arch, codename


def apt_environment(work, codename, arch):
    uri = "https://archive.ubuntu.com/ubuntu" if arch == "amd64" else "https://ports.ubuntu.com/ubuntu-ports"
    security = "https://security.ubuntu.com/ubuntu" if arch == "amd64" else uri
    (work / "lists/partial").mkdir(parents=True)
    (work / "empty").mkdir()
    sources = work / "sources.list"
    sources.write_text("".join(f"deb [arch={arch} signed-by={KEYRING}] {mirror} {suite} main\n"
                               for mirror, suite in ((uri, codename), (uri, codename+"-updates"), (security, codename+"-security"))))
    conf = work / "apt.conf"
    conf.write_text(f'''Dir::Etc::main "/dev/null";
Dir::Etc::parts "{work}/empty";
Dir::Etc::sourcelist "{sources}";
Dir::Etc::sourceparts "{work}/empty";
Dir::Etc::preferences "/dev/null";
Dir::Etc::preferencesparts "{work}/empty";
Dir::State::lists "{work}/lists";
Dir::Cache::pkgcache "";
Dir::Cache::srcpkgcache "";
Acquire::AllowInsecureRepositories "false";
Acquire::https::AllowRedirect "false";
Acquire::AllowWeakRepositories "false";
Acquire::AllowDowngradeToInsecureRepositories "false";
APT::Get::AllowUnauthenticated "false";
APT::Update::Error-Mode "any";
''')
    return {**os.environ, "APT_CONFIG": str(conf)}


def package_metadata(records, name, version, arch):
    for stanza in records.split("\n\n"):
        fields = dict(line.split(": ", 1) for line in stanza.splitlines() if line and not line.startswith(" ") and ": " in line)
        if (fields.get("Package"), fields.get("Version"), fields.get("Architecture")) == (name, version, arch):
            if re.fullmatch(r"[a-f0-9]{64}", fields.get("SHA256", "")) and fields.get("Filename", "").startswith("pool/main/"):
                return fields["SHA256"]
    return None


def download_package(work, env, name, version, arch, digest, release):
    import pwd
    dest = work / name
    dest.mkdir()
    os.chown(dest, pwd.getpwnam("_apt").pw_uid, -1)
    run(["/usr/bin/apt-get", "download", f"{name}:{arch}={version}"], cwd=dest, env=env)
    downloaded, = dest.glob("*.deb")
    archive = work / (name + ".deb")
    # dpkg opens the path again after verification. Keep the checked copy
    # outside the APT download account's writable directory.
    with os.fdopen(os.open(downloaded, os.O_RDONLY | os.O_NOFOLLOW), "rb") as src, archive.open("xb") as out:
        shutil.copyfileobj(src, out)
    sha = hashlib.sha256()
    with archive.open("rb") as src:
        for chunk in iter(lambda: src.read(1 << 20), b""):
            sha.update(chunk)
    if sha.hexdigest() != digest:
        raise ValueError("签名索引 SHA256 不匹配：" + name)
    for key, expected in (("Package", name), ("Version", version), ("Architecture", arch)):
        if run(["/usr/bin/dpkg-deb", "-f", archive, key]) != expected:
            raise ValueError("归档 " + key + " 不匹配：" + name)
    with subprocess.Popen(["/usr/bin/dpkg-deb", "--fsys-tarfile", archive], stdout=subprocess.PIPE) as proc:
        with tarfile.open(fileobj=proc.stdout, mode="r|") as tar:
            provided = inspect_payload(tar, release)
        # tarfile stops after the end marker. Drain any final block padding.
        while proc.stdout.read(65536):
            pass
        if proc.wait():
            raise ValueError("读取包数据清单失败：" + name)
    return archive, provided


@contextmanager
def frontend_lock(path):
    import fcntl
    with open(path, "a+b") as lock:
        try:
            fcntl.lockf(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as exc:
            raise RuntimeError(f"dpkg 包事务锁已占用：{exc}") from exc
        yield


def install(targets):
    if not targets or set(targets) - TARGETS.keys():
        raise ValueError("只接受 bbr 和 fq")
    release, version, arch, codename = host_identity()
    with tempfile.TemporaryDirectory(prefix="qui-modules-") as tmp:
        work = Path(tmp)
        work.chmod(0o711)
        env = apt_environment(work, codename, arch)
        run(["/usr/bin/apt-get", "update"], env=env)
        needed = set(targets)
        selected = []
        for prefix in ["linux-modules-", "linux-modules-extra-"]:
            name = prefix + release
            # An exact build can have aged out of every signed index.
            names = run(["/usr/bin/apt-cache", "pkgnames", name], env=env).splitlines()
            if name not in names:
                continue
            records = run(["/usr/bin/apt-cache", "show", name + ":" + arch], env=env)
            digest = package_metadata(records, name, version, arch)
            if not digest:
                continue
            archive, provided = download_package(work, env, name, version, arch, digest, release)
            if needed & provided:
                selected.append((name, archive))
                needed -= provided
            if not needed:
                break
        if needed:
            raise ValueError(f"Ubuntu 官方索引没有提供 {release} {version} {arch} 的匹配独立模块包：{sorted(needed)}。请在宿主检查，如需换内核请由 Owner 处理。")
        # dpkg uses POSIX record locks. Keep the frontend lock through the dry
        # run, all actual package operations and the final dpkg audit.
        with frontend_lock("/var/lib/dpkg/lock-frontend"):
            audit = run(["/usr/bin/dpkg", "--audit"])
            if audit:
                raise ValueError("dpkg 状态不完整：" + audit)
            if host_identity() != (release, version, arch, codename):
                raise ValueError("运行内核已变更")
            for name, archive in selected:
                plan = run(["/usr/bin/apt-get", "-s", "--no-remove", "--no-install-recommends", "--reinstall", "install", archive], env=env)
                check_plan(plan, name)
            for name, archive in selected:
                print(f"配置 {name} {version} {arch}", file=sys.stderr, flush=True)
                run(["/usr/bin/dpkg", "--triggers", "--install", archive], env={**env, "DPKG_FRONTEND_LOCKED": "true", "DEBIAN_FRONTEND": "noninteractive"})
                status = run(["/usr/bin/dpkg-query", "-W", "-f=${db:Status-Status} ${Version} ${Architecture}", name])
                if status != f"installed {version} {arch}":
                    raise ValueError(f"{name} 配置或触发器未完成：{status}")
                print(name, flush=True)
            audit = run(["/usr/bin/dpkg", "--audit"])
            if audit:
                raise ValueError("配置或触发器未完成：" + audit)


if __name__ == "__main__":
    if sys.argv[1:] == ["probe"]:
        host_identity()
    else:
        install(sys.argv[1:])
