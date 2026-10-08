#!/usr/bin/env python3
"""Relecture indépendante de shortcuts.vdf.

Aucune ligne de ce script ne partage de code avec le programme Go : il relit le
fichier octet par octet, vérifie la structure binaire du format Valve, les
champs attendus par Steam, et recalcule l'appid avec zlib plutôt qu'avec le
crc32 de Go. C'est ce qui en fait une vérification, et non un test qui se
contenterait de confirmer sa propre implémentation.

Usage : verif_vdf.py <chemin/shortcuts.vdf>
"""
import struct
import sys
import zlib

TYPES = {0x00: "objet", 0x01: "chaîne", 0x02: "int32"}

# Champs qu'un raccourci non-Steam doit porter pour que Steam l'affiche et le
# lance correctement.
CHAMPS_REQUIS = ["AppName", "Exe", "StartDir", "LaunchOptions", "AllowDesktopConfig", "AllowOverlay"]


def parse(buf, i=0, out=None):
    if out is None:
        out = {}
    while i < len(buf):
        t = buf[i]
        i += 1
        if t == 0x08:
            break
        if t not in TYPES:
            raise SystemExit("type inconnu 0x%02x à l'offset %d" % (t, i - 1))
        j = buf.index(b"\x00", i)
        key = buf[i:j].decode()
        i = j + 1
        if t == 0x00:
            out[key] = {}
            i = parse(buf, i, out[key])
        elif t == 0x01:
            j = buf.index(b"\x00", i)
            out[key] = buf[i:j].decode()
            i = j + 1
        else:
            out[key] = struct.unpack("<I", buf[i : i + 4])[0]
            i += 4
    return i


def main():
    if len(sys.argv) != 2:
        raise SystemExit(__doc__)
    path = sys.argv[1]
    data = open(path, "rb").read()

    print("fichier       :", path)
    print("taille        :", len(data), "octets")
    print("octet initial :", hex(data[0]), "(0x00 = objet racine)")
    print("fin de fichier:", data[-2:].hex(), "(attendu 0808)")

    root = {}
    parse(data, 0, root)
    if list(root) != ["shortcuts"]:
        raise SystemExit("racine inattendue : %s" % list(root))

    entries = root["shortcuts"]
    print("entrées       :", len(entries))

    ok = True
    for idx, sc in entries.items():
        appid = sc.get("appid")
        nom = sc.get("AppName")
        exe = sc.get("Exe")
        ref = (zlib.crc32((exe + nom).encode()) | 0x80000000) & 0xFFFFFFFF
        conforme = appid == ref
        ok = ok and conforme

        print()
        print("  #%s" % idx)
        print("    AppName        :", nom)
        print("    Exe            :", exe)
        print("    StartDir       :", sc.get("StartDir"))
        print("    LaunchOptions  :", sc.get("LaunchOptions"))
        print("    appid stocké   :", appid)
        print("    appid recalculé:", ref, "->", "conforme" if conforme else "NON CONFORME")
        print("    champs         :", sorted(sc.keys()))
        for champ in CHAMPS_REQUIS:
            if champ not in sc:
                print("    MANQUANT       :", champ)
                ok = False

    print()
    print("résultat :", "conforme" if ok else "NON CONFORME")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
