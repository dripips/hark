# Hark

Ein selbst gehosteter KI-Chatbot für die eigene Website, bei dem jede Antwort einen Beleg mitbringt.

[English](README.md) · [Русский](README.ru.md)

![Der Beleg zu einer Antwort](screenshots/01-receipt.png)

## Worum es geht

Baukästen für Chatbots gibt es reichlich, und fast alle sind Blackboxes. Der Bot sagt einem Kunden etwas Falsches, und niemand kann rekonstruieren, warum: welche Anweisung griff, was die API zurückgab, was das Modell überhaupt gelesen hat. Die Person, die das Gespräch übernimmt, erbt denselben Nebel.

Hark beantwortet diese Frage auf dem Bildschirm. Jede Antwort lässt sich aufklappen: die Anweisung, jeder Werkzeugaufruf mit Anfrage und Rückgabe, die aus der Datenbank gelesenen Zeilen, Token getrennt nach Eingabe, Ausgabe und Denken, und was das gekostet hat.

Kein SaaS, keine Tarife, keine Plätze. Eine Binärdatei, daneben eine Datei.

## Nicht an ein Modell gebunden

Der Bot läuft auf OpenAI, auf Anthropic und auf allem, was das OpenAI-Format spricht: Ollama, vLLM, LM Studio, OpenRouter, der eigene Server. Der Wechsel sind zwei Felder in den Einstellungen.

Die Anbieter sind sich uneinig darüber, was sie annehmen, und diese Uneinigkeit steht in keiner Dokumentation. `gpt-5-nano` lehnt `temperature` rundweg ab: „Nur der Standardwert wird unterstützt." Das alte Feld `max_tokens` ebenfalls. Ein Baukasten mit Temperatur-Schieberegler zerlegt bei diesem Modell jede Anfrage.

Also fragt Hark nach. Ein Knopf schickt einige winzige Anfragen und hält fest, was zurückkam:

![Einstellungen nach der Prüfung](screenshots/11-model.png)

Danach zeigen die Einstellungen nur noch die Regler, die dieses Modell wirklich annimmt. Nichts anderes wird angeboten, nichts Abgelehntes je gesendet.

## Das Denken ist der größere Teil der Rechnung

Denkende Modelle verbrauchen Token, die niemand zu sehen bekommt. Gemessen an einem echten Gespräch durch dieses Produkt:

| | Token |
|---|---|
| Eingabe | 777 |
| Ausgabe | 288 |
| davon Denken | **192** |

Zwei Drittel der bezahlten Ausgabe stehen nicht in der Antwort. Hark führt die Denk-Token in einer eigenen Spalte und zeigt ihren Anteil im Gespräch und in der Auswertung: Ein Kostenbericht, der sie in „Ausgabe" einrechnet, ist weniger falsch als unbrauchbar.

![Ausgabe-Token pro Tag, Denken getrennt](screenshots/04-analytics.png)

## Anbindungen

Eigene API und Datenbank liegen im Reiter „Anbindungen“, der auch als Erstes aufgeht, wenn man einen Bot öffnet. Die Art wird vor dem Formular gewählt, mit zwei Schaltflächen, damit nie überflüssige Felder auf dem Schirm stehen.

![Anbindungen](screenshots/08-connections.png)

Die Schaltfläche „Prüfen“ ruft die Anbindung wirklich auf und zeigt, was zurückkam. Ein Tippfehler in der Verbindungszeichenfolge fällt hier auf und nicht mitten im Gespräch mit einem Besucher.

**HTTP.** Beliebige Adresse. Methode, Vorlage mit `{Platzhaltern}`, Kopfzeilen, Rumpf. Die Argumente des Modells werden in Pfad und Abfrage eingesetzt.

**SQL.** Ein Zugang zur eigenen Datenbank, nur lesend. Die Abfrage schreibt das Modell, ob sie läuft entscheidet Hark:

1. nur `SELECT` und `WITH … SELECT`;
2. keine zweite Anweisung nach dem Semikolon;
3. kein `INSERT`, `DROP`, `ATTACH`, `PRAGMA`, `load_file`, `INTO OUTFILE` und dergleichen;
4. Tabellen gegen eine Positivliste geprüft, Unterabfragen eingeschlossen;
5. die Zeilengrenze durch Umschließen der Abfrage, nicht durch angehängtes `LIMIT` — das umgeht ein `UNION`;
6. eine Zeitgrenze für die Ausführung.

Das ist Tiefenstaffelung, kein Ersatz für Rechte. Verbinden Sie mit einem Benutzer ohne Schreibrecht; im Formular steht es genau so.

Eine abgelehnte Abfrage wird nicht verschwiegen: Sie landet im Beleg, und man sieht, dass der Bot eine Tabelle lesen wollte, die ihn nichts angeht.

## Übergabe an einen Menschen

Der Bot gibt per Werkzeugaufruf auf, nicht per Phrasenabgleich. Er sagt es ausdrücklich, der Grund wandert in die Warteschlange, das Gespräch wechselt in den Wartezustand. Die zuständige Person sieht Grund und vollständigen Beleg, bevor sie ein Wort schreibt.

![Die Warteschlange](screenshots/03-inbox.png)

## Wenn der Bot aufgibt

Das Gespräch reiht sich in die Warteschlange für einen Menschen ein, und davon erfährt man gleich auf zwei Wegen.

Ein offener Reiter der Verwaltung bekommt das Ereignis als Strom: die Zahl in der Kopfzeile wächst, der Reitertitel wird zu „(3) Übersicht“, ein kurzer Ton erklingt. Einzustellen gibt es dafür nichts, es läuft nach der Installation. Das Ereignis trägt nicht „einer mehr“, sondern die ganze Schlange, deshalb heilen ein verlorenes Ereignis, ein eingeschlafener Rechner und ein Neustart des Servers von selbst.

![Die Warteschlange in der Kopfzeile](screenshots/17-queue-badge.png)

Hält niemand die Verwaltung offen, ruft Hark eine einzige Adresse auf, die Sie selbst eingetragen haben. Telegram und E-Mail stecken nicht in Hark und werden es nicht: Telegram entsteht dadurch, dass Sie `api.telegram.org/bot<Token>/sendMessage` einsetzen, E-Mail dadurch, dass daneben Ihre eigene Brücke steht. Die Schaltfläche neben dem Feld ruft wirklich an und zeigt, was zurückkam.

![Wohin melden](screenshots/18-notify.png)

Nach außen geht die Tatsache, nicht das Gespräch: Bot, Grund, Länge der Schlange und ein Link in die Verwaltung. Den Grund schreibt das Modell, und er kann die Worte des Besuchers wiedergeben — das Häkchen „ohne Grund“ lässt nur Zähler und Link übrig.

Ein wartendes Gespräch hat jemanden, der es übernommen hat. Die Schaltfläche „Ich nehme es“ schreibt es auf Sie, und die Kollegen sehen den Namen direkt in der Liste — bevor sie zu tippen anfangen und nicht erst, nachdem der Besucher zwei Antworten von zwei Leuten bekommen hat. Das Übernehmen ist atomar: Zwei gleichzeitige Klicks ergeben genau einen Gewinner, der andere sieht, wer schneller war.

![Wer das Gespräch übernommen hat](screenshots/20-claim.png)

Freigeben darf jeder, nicht nur der Übernehmende: Menschen gehen essen, und einen Besucher hinter jemandem einzusperren, der weg ist, wäre das Schlechteste überhaupt. Die Antwort eines Betreuers übernimmt das Gespräch von selbst, falls es frei war; Rückgabe an den Bot und Schließen löschen die Markierung.

Ob eine Eskalation festgehalten wird, hängt nicht mehr davon ab, ob die Anfrage des Besuchers noch lebt. Ein mitten im Satz geschlossener Reiter führte früher zum denkbar schlechtesten Ausgang: Der Besucher bekam „Ich übergebe an einen Menschen“, das Gespräch blieb außerhalb der Schlange, und den versprochenen Menschen rief niemand.

## Betreuer

Wer sich in die Verwaltung einloggt und antwortet, wenn der Bot aufgibt. Rollen gibt es nicht: Jeder sieht alle Gespräche und kann jeden anderen entfernen.

![Betreuer](screenshots/15-managers.png)

Sich selbst kann man nicht entfernen und den letzten Betreuer auch nicht: Danach käme niemand mehr hinein, und die Rettung ginge nur noch über den Server selbst. Ein Passwortwechsel beendet die übrigen Sitzungen dieser Person, denn man wechselt es oft gerade deshalb, weil jemand anderes es hat.

Den ersten Betreuer legt man auf der Kommandozeile an:

```bash
./hark -manager you@example.com -password geheim
./hark -managers                                        # wer schon da ist
./hark -manager you@example.com -password neu -reset    # Passwort ändern
```

Ohne `-reset` verweigert ein zweiter Aufruf mit derselben Adresse den Dienst und sagt, was stattdessen zu tun ist. Früher änderte er das Passwort stillschweigend und sperrte die Person aus.

## Das Widget

Ein Tag auf der Seite:

```html
<script src="https://hark.example.com/widget/hark.js" data-bot="shop" defer></script>
```

23 KB, über die Leitung sieben: Hark liefert ihn gzip-komprimiert. Ohne Abhängigkeiten, das Markup in einem Shadow Root — CSS läuft in keine Richtung aus. Die Antwort kommt als Strom über SSE, Antworten der Betreuung werden per Abfrage nachgeholt.

Jeder Teil des Widgets ist optional. Füllt man nichts aus, bleibt ein nackter Verlauf mit Eingabefeld; füllt man alles aus, bekommt man einen runden Starter, einen Begrüßungsschirm mit vorbereiteten Fragen und eine Fußnote mit Link zur Datenschutzseite.

![Das Widget auf einer fremden Seite](screenshots/05-widget.png)

Die erlaubten Domains werden je Bot festgelegt. Eine leere Liste heißt: jede Seite.

## Aussehen

Schrift, Abstände, Farben und Hintergrund stehen im Reiter „Aussehen“: eine Schriftauswahl samt „wie auf der Seite“, Schriftgrad und Zeilenabstand, drei Dichten, Fenstermaße, Rundungen, Schatten oder Haarlinie, die Palette und ein Verlaufshintergrund als Fläche, Verlauf, Punkte, Raster oder Bild. Fünf Vorlagen setzen alles auf einmal.

![Werkstatt für das Aussehen](screenshots/10-widget-studio.png)

![Ein eingestelltes Widget auf einer echten Seite](screenshots/14-widget-live.png)

Drei Farben werden gesetzt, der Rest folgt: gedämpfter Text, Haarlinien und die Blase des Bots werden aus Fläche und Textfarbe berechnet, die Beschriftung auf einer Füllung nach Kontrast. Wer das Häkchen „auto“ entfernt, übernimmt die Farbe selbst.

Werte des Themas landen direkt im CSS des Widgets und werden deshalb an einer Stelle geprüft: eine Farbe muss ein sechsstelliger Code sein, eine Adresse `https`, Zahlen werden begrenzt, unbekannte Varianten fallen auf die Vorgabe zurück.

Im Rahmen rechts läuft das Widget selbst, kein Entwurf — dieselbe Datei und derselbe Endpunkt für die Einstellungen, die auch die Seite bekommt. Auseinanderlaufen kann da nichts.

## Sprachen

Verwaltung und Widget sprechen Russisch und Englisch. Die Sprache der Oberfläche wählt jeder Betreuer für sich: In einem Team sitzen mitunter ein russisch- und ein englischsprachiger Mensch, und eine gemeinsame Einstellung würde einen von beiden leiden lassen.

Die Sprache des Bots wird getrennt gesetzt, im Reiter „Wie er antwortet“. Sie bestimmt die Beschriftungen im Widget und die Regeln, die das Modell liest: Russische Regeln ziehen eine russische Antwort selbst auf einer englischen Seite, und daran ändern Anweisungen nichts.

Eine Sprache hinzuzufügen heißt, eine Datei zu übersetzen:

```bash
go run ./cmd/locale            # was da ist und was fehlt
go run ./cmd/locale fr > internal/lang/locales/fr.json
```

Der Schlüssel der Übersetzung ist die russische Ausgangszeile selbst, wie bei gettext. So sieht man in der Vorlage, was ausgegeben wird, ohne im Wörterbuch nachzuschlagen, und eine nicht übersetzte Zeile bleibt russisch, statt zu `nav.bots` zu werden. Einen Preis hat das: Ändert man die russische Formulierung, reißt die Verbindung zur Übersetzung. `go test ./internal/lang` fängt das ab und meldet sowohl Lücken als auch verwaiste Zeilen.

## Installation

```bash
go build -o hark .
./hark -manager du@example.com -password geheim
./hark
```

Dann `http://localhost:8080` öffnen. Oberfläche, Vorlagen und Widget stecken in der Binärdatei, die Datenbank liegt als Datei daneben.

Zum Umsehen:

```bash
./hark -demo     # ein Laden, zwei Anbindungen, vier Gespräche mit Belegen
```

Die Demo läuft ohne API-Schlüssel und kostet nichts: Die Belege sind aufgezeichnet, nicht erzeugt.

Oder im Container:

```bash
docker compose up -d
docker compose run --rm hark -manager you@example.com -password geheim
```

Das Abbild wiegt 42 MB, der Prozess läuft nicht als root, die Datenbank liegt in einem benannten Volume. SQLite ist hier reines Go, cgo entfällt, die Binärdatei ist statisch und lässt sich ohne die übliche Mühe für arm64 auf einem Raspberry Pi bauen.


## Einstellungen

| Schalter | Variable | Vorgabe |
|---|---|---|
| `-addr` | `HARK_ADDR` | `:8080` |
| `-db` | `HARK_DB` | `hark.db` |

Die Modellschlüssel liegen je Bot in der Datenbank, nicht in der Umgebung: Verschiedene Bots dürfen verschiedene Anbieter nutzen.

## Aufbau

```
internal/llm      Anbieter und Fähigkeitsprüfung
internal/tools    HTTP- und SQL-Werkzeuge mit ihren Sperren
internal/chat     die Gesprächsschleife, die den Beleg schreibt
internal/store    SQLite-Schema und Abfragen
internal/web      Oberfläche, Widget-API, Vorlagen und das Widget selbst
```

## Tests

```bash
go test ./...
```

Schleife, Beleg und SQL-Sperren werden ohne Netz geprüft: Ein vorgetäuschter Anbieter spielt ein aufgezeichnetes Gespräch ab, während das HTTP-Werkzeug einen echten Server neben dem Test aufruft.

Tests gegen einen echten Anbieter werden übersprungen, solange man sie nicht anfordert:

```bash
HARK_LIVE_KEY=... HARK_LIVE_MODEL=gpt-5-nano go test ./internal/llm -run Live -v
```

## Lizenz

MIT.
