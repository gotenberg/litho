"""Save documents with a password through LibreOffice's UNO API.

Each argument is "src,out,filter,odf_version,password": src is loaded and
stored to out with the filter and the password, after setting the ODF
version LibreOffice writes (org.openoffice.Office.Common/Save/ODF/
DefaultVersion; empty keeps the default), which picks the encryption: 2
(ODF 1.1) writes Blowfish, 10 (ODF 1.3) AES-256-CBC, the default (ODF 1.4
extended) AES-256-GCM with Argon2id over the whole package.
"""

import subprocess
import sys
import time

import uno
from com.sun.star.beans import PropertyValue


def prop(name, value):
    p = PropertyValue()
    p.Name = name
    p.Value = value
    return p


def main(jobs):
    office = subprocess.Popen(
        [
            "/usr/lib/libreoffice/program/soffice",
            "--headless",
            "--invisible",
            "--norestore",
            "-env:UserInstallation=file:///tmp/offcrypto-profile",
            "--accept=pipe,name=offcrypto;urp;",
        ]
    )
    local = uno.getComponentContext()
    resolver = local.ServiceManager.createInstanceWithContext(
        "com.sun.star.bridge.UnoUrlResolver", local
    )
    ctx = None
    for _ in range(60):
        try:
            ctx = resolver.resolve(
                "uno:pipe,name=offcrypto;urp;StarOffice.ComponentContext"
            )
            break
        except Exception:  # noqa: BLE001 - LibreOffice is still starting
            time.sleep(1)
    if ctx is None:
        sys.exit("LibreOffice did not start")
    smgr = ctx.ServiceManager
    desktop = smgr.createInstanceWithContext("com.sun.star.frame.Desktop", ctx)
    config = smgr.createInstanceWithContext(
        "com.sun.star.configuration.ConfigurationProvider", ctx
    )
    default = None
    for job in jobs:
        src, out, filt, version, password = job.split(",")
        access = config.createInstanceWithArguments(
            "com.sun.star.configuration.ConfigurationUpdateAccess",
            (prop("nodepath", "/org.openoffice.Office.Common/Save/ODF"),),
        )
        if default is None:
            default = access.getPropertyValue("DefaultVersion")
        access.setPropertyValue("DefaultVersion", int(version) if version else default)
        access.commitChanges()
        doc = desktop.loadComponentFromURL(
            "file://" + src, "_blank", 0, (prop("Hidden", True),)
        )
        doc.storeToURL("file://" + out, (prop("FilterName", filt), prop("Password", password)))
        doc.close(True)
        print("wrote", out, flush=True)
    try:
        desktop.terminate()
    except Exception:  # noqa: BLE001 - the bridge drops as LibreOffice exits
        pass
    office.wait(timeout=60)


if __name__ == "__main__":
    main(sys.argv[1:])
