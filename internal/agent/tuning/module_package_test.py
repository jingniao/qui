import importlib.util
from pathlib import Path
import tarfile
import tempfile
import subprocess
import sys
import unittest
from contextlib import nullcontext
from unittest.mock import patch


class ModulePackageTest(unittest.TestCase):
    def setUp(self):
        spec = importlib.util.spec_from_file_location("ubuntu_modules", Path(__file__).with_name("ubuntu_modules.py"))
        self.pkg = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.pkg)

    @unittest.skipUnless(sys.platform == "linux", "dpkg uses POSIX locks")
    def test_real_frontend_lock_excludes_second_process(self):
        with tempfile.TemporaryDirectory() as directory:
            path = str(Path(directory) / "lock-frontend")
            child = [sys.executable, "-IB", "-c",
                     "import runpy,sys; lock=runpy.run_path(sys.argv[1])['frontend_lock'];\nwith lock(sys.argv[2]): pass",
                     self.pkg.__file__, path]
            with self.pkg.frontend_lock(path):
                result = subprocess.run(child, capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("dpkg 包事务锁已占用", result.stderr)
            subprocess.run(child, check=True)

    def test_running_identity(self):
        release = "6.8.0-100-generic"
        signature = "Ubuntu 6.8.0-100.100-generic 6.8.12"
        self.assertEqual(self.pkg.build_version(release, signature), "6.8.0-100.100")
        for bad in ["Ubuntu 6.8.0-99.100-generic 6.8.12", "Ubuntu 6.8.0-100.100-aws 6.8.12", "Linux " + release]:
            with self.assertRaises(ValueError):
                self.pkg.build_version(release, bad)

    def test_archive_contains_only_current_module_tree_and_boot_metadata(self):
        release = "6.8.0-100-generic"
        target = "./lib/modules/" + release + "/kernel/net/ipv4/tcp_bbr.ko.zst"
        entries = [tarfile.TarInfo(target), tarfile.TarInfo("./boot/config-" + release)]
        self.assertEqual(self.pkg.inspect_payload(entries, release), {"bbr"})
        for path in ["./boot/vmlinuz-" + release, "./usr/lib/modules/6.8.0-99-generic/kernel/net/ipv4/tcp_bbr.ko", "./etc/apt/sources.list"]:
            with self.assertRaises(ValueError):
                self.pkg.inspect_payload(entries + [tarfile.TarInfo(path)], release)

    def test_dependency_transaction(self):
        name = "linux-modules-6.8.0-100-generic"
        self.pkg.check_plan(f"Inst {name} [1] (1 Ubuntu:24.04/noble [amd64])\nConf {name} (1 Ubuntu:24.04/noble [amd64])", name)
        for line in ["Inst linux-image-6.8.0-101-generic (2)", "Remv obsolete", "Conf pending-package (2)"]:
            with self.assertRaises(ValueError):
                self.pkg.check_plan(f"Inst {name} (1)\n" + line, name)

    def test_only_exact_signed_index_record_supplies_digest(self):
        name = "linux-modules-6.8.0-100-generic"
        digest = "f" * 64
        record = f"Package: {name}\nVersion: 6.8.0-100.100\nArchitecture: amd64\nFilename: pool/main/l/linux/pkg.deb\nSHA256: {digest}"
        self.assertEqual(self.pkg.package_metadata(record, name, "6.8.0-100.100", "amd64"), digest)
        for old, new in [("amd64", "arm64"), ("100.100", "100.101"), ("pool/main/", "pool/universe/"),
                         ("linux-modules-", "linux-image-"), ("SHA256:", "MD5sum:")]:
            self.assertIsNone(self.pkg.package_metadata(record.replace(old, new), name, "6.8.0-100.100", "amd64"))

    def test_archive_sources_and_configuration_are_isolated(self):
        for arch, mirror in [("amd64", "archive.ubuntu.com"), ("arm64", "ports.ubuntu.com")]:
            with tempfile.TemporaryDirectory() as directory:
                work = Path(directory)
                env = self.pkg.apt_environment(work, "noble", arch)
                conf = Path(env["APT_CONFIG"]).read_text()
                self.assertIn(f'Dir::Etc::parts "{work}/empty";', conf)
                self.assertIn('Dir::Etc::main "/dev/null";', conf)
                sources = (work / "sources.list").read_text().splitlines()
                self.assertEqual(len(sources), 3)
                self.assertTrue(all("signed-by=/usr/share/keyrings/ubuntu-archive-keyring.gpg" in line for line in sources))
                self.assertIn("https://" + mirror, sources[0])

    def test_fixed_dpkg_call_after_dependency_check(self):
        release, version = "6.8.0-100-generic", "6.8.0-100.100"
        name = "linux-modules-" + release
        record = f"Package: {name}\nVersion: {version}\nArchitecture: amd64\nFilename: pool/main/l/linux/test.deb\nSHA256: " + "a" * 64
        for extra_dependency in [True, False]:
            installed = []
            def command(args, **kwargs):
                if args[:2] == ["/usr/bin/apt-get", "update"]:
                    return ""
                if args[:2] == ["/usr/bin/apt-cache", "pkgnames"]:
                    return name
                if args[:2] == ["/usr/bin/apt-cache", "show"]:
                    return record
                if args == ["/usr/bin/dpkg", "--audit"]:
                    return ""
                if args[:2] == ["/usr/bin/apt-get", "-s"]:
                    self.assertEqual(args[2:-1], ["--no-remove", "--no-install-recommends", "--reinstall", "install"])
                    return f"Inst {name} ({version})\nConf {name} ({version})" + ("\nInst linux-image-generic (1)" if extra_dependency else "")
                if args[:3] == ["/usr/bin/dpkg", "--triggers", "--install"]:
                    self.assertEqual(kwargs["env"]["DPKG_FRONTEND_LOCKED"], "true")
                    installed.append(Path(args[3]).name)
                    return ""
                if args[0] == "/usr/bin/dpkg-query":
                    return f"installed {version} amd64"
                self.fail(f"unexpected command: {args}")
            with patch.object(self.pkg, "host_identity", return_value=(release, version, "amd64", "noble")), \
                 patch.object(self.pkg, "run", side_effect=command), \
                 patch.object(self.pkg, "download_package", side_effect=lambda work,*_: (work / (name+".deb"), {"bbr","fq"})), \
                 patch.object(self.pkg, "frontend_lock", return_value=nullcontext()):
                if extra_dependency:
                    with self.assertRaisesRegex(ValueError, "依赖预检"):
                        self.pkg.install(["fq"])
                    self.assertEqual(installed, [])
                else:
                    self.pkg.install(["fq","bbr"])
                    self.assertEqual(installed, [name+".deb"])


if __name__ == "__main__":
    unittest.main()
