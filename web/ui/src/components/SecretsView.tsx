import { useEffect, useState } from "react";
import {
  cipherRecipients,
  decryptSecret,
  encryptSecret,
  errorMessage,
  SECRET_FORMATS,
} from "../api.ts";
import { useToast } from "./Toast.tsx";
import { Button, SelectField } from "./ui.tsx";

const textareaClass =
  "w-full rounded-lg border border-slate-300 bg-white p-3 font-mono text-xs text-slate-800 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100";

function OutputBlock({ label, value }: { label: string; value: string }) {
  const toast = useToast();

  if (value === "") {
    return null;
  }

  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium text-slate-500 dark:text-slate-400">{label}</span>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            navigator.clipboard
              ?.writeText(value)
              .then(() => toast.success("Copied"))
              .catch(() => toast.error("Clipboard unavailable"));
          }}
        >
          Copy
        </Button>
      </div>
      <pre className={`${textareaClass} max-h-72 overflow-auto whitespace-pre-wrap`}>{value}</pre>
    </div>
  );
}

// SecretsView is a local SOPS tool: encrypt a plaintext document for an age recipient, and decrypt a
// SOPS document with the local age keys. Shown only when the backend advertises secretsCipher (the
// local `ksail ui`/desktop backend); the operator has no local keys.
export function SecretsView() {
  const toast = useToast();
  const [recipients, setRecipients] = useState<string[]>([]);
  const [format, setFormat] = useState("yaml");

  const [recipient, setRecipient] = useState("");
  const [plaintext, setPlaintext] = useState("");
  const [encrypted, setEncrypted] = useState("");
  const [encryptBusy, setEncryptBusy] = useState(false);

  const [encryptedInput, setEncryptedInput] = useState("");
  const [decrypted, setDecrypted] = useState("");
  const [decryptBusy, setDecryptBusy] = useState(false);

  useEffect(() => {
    cipherRecipients()
      .then((response) => setRecipients(response.recipients))
      .catch(() => {
        // Non-fatal: the recipient selector just falls back to the backend default.
      });
  }, []);

  function runEncrypt() {
    if (plaintext.trim() === "") {
      toast.error("Enter a plaintext document to encrypt");

      return;
    }

    setEncryptBusy(true);
    setEncrypted("");
    encryptSecret(plaintext, recipient, format)
      .then((response) => {
        setEncrypted(response.encrypted);
        toast.success("Encrypted");
      })
      .catch((err: unknown) => toast.error(errorMessage(err)))
      .finally(() => setEncryptBusy(false));
  }

  function runDecrypt() {
    if (encryptedInput.trim() === "") {
      toast.error("Paste a SOPS-encrypted document to decrypt");

      return;
    }

    setDecryptBusy(true);
    setDecrypted("");
    decryptSecret(encryptedInput, format)
      .then((response) => {
        setDecrypted(response.plaintext);
        toast.success("Decrypted");
      })
      .catch((err: unknown) => toast.error(errorMessage(err)))
      .finally(() => setDecryptBusy(false));
  }

  return (
    <div className="mx-auto max-w-5xl space-y-5">
      <div className="flex items-end gap-3">
        <SelectField
          label="Format"
          value={format}
          onChange={(event) => setFormat(event.target.value)}
          className="min-w-32"
        >
          {SECRET_FORMATS.map((value) => (
            <option key={value} value={value}>
              {value.toUpperCase()}
            </option>
          ))}
        </SelectField>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <section className="space-y-3 rounded-xl border border-slate-200 p-4 dark:border-slate-800">
          <h2 className="text-sm font-semibold text-slate-900 dark:text-white">Encrypt</h2>
          <SelectField
            label="Recipient (age public key)"
            value={recipient}
            onChange={(event) => setRecipient(event.target.value)}
          >
            <option value="">Default (local key)</option>
            {recipients.map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </SelectField>
          <textarea
            value={plaintext}
            onChange={(event) => setPlaintext(event.target.value)}
            placeholder={"password: s3cret\napiKey: abc123"}
            spellCheck={false}
            rows={10}
            className={textareaClass}
          />
          <Button onClick={runEncrypt} loading={encryptBusy}>
            Encrypt
          </Button>
          <OutputBlock label="Encrypted (SOPS)" value={encrypted} />
        </section>

        <section className="space-y-3 rounded-xl border border-slate-200 p-4 dark:border-slate-800">
          <h2 className="text-sm font-semibold text-slate-900 dark:text-white">Decrypt</h2>
          <textarea
            value={encryptedInput}
            onChange={(event) => setEncryptedInput(event.target.value)}
            placeholder="Paste a SOPS-encrypted document…"
            spellCheck={false}
            rows={10}
            className={textareaClass}
          />
          <Button onClick={runDecrypt} loading={decryptBusy}>
            Decrypt
          </Button>
          <OutputBlock label="Plaintext" value={decrypted} />
        </section>
      </div>
    </div>
  );
}
