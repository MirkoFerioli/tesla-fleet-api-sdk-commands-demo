@echo off
setlocal
cd /d "%~dp0" || exit /b 1

if /i "%~1"=="prepara" goto prepara
if /i "%~1"=="installa" goto installa
if /i "%~1"=="avvia" goto avvia
if /i "%~1"=="ferma" goto ferma
if /i "%~1"=="stato" goto stato
if /i "%~1"=="hostname" goto hostname
if /i "%~1"=="verifica" goto verifica
if /i "%~1"=="log" goto log
if /i "%~1"=="login-tesla" goto login_tesla
if /i "%~1"=="attiva-telemetria" goto attiva_telemetria
if /i "%~1"=="stato-telemetria" goto stato_telemetria
if /i "%~1"=="disattiva-telemetria" goto disattiva_telemetria
if /i "%~1"=="backup-db" goto backup_db

echo Uso: tesla.cmd ^<prepara^|installa^|avvia^|ferma^|stato^|hostname^|verifica^|log [servizio]^|login-tesla^|attiva-telemetria^|stato-telemetria^|disattiva-telemetria^|backup-db^>
exit /b 2

:prepara
if exist .env (
  echo Il file .env esiste gia; non e stato modificato.
  exit /b 1
)
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "$content = [IO.File]::ReadAllText('.env.example'); while ($content.Contains('__GENERATE__')) { $content = $content.Replace('__GENERATE__', [guid]::NewGuid().ToString('N')) }; [IO.File]::WriteAllText('.env', $content, [Text.Encoding]::ASCII)"
if errorlevel 1 exit /b 1
notepad.exe .env
exit /b 0

:installa
if not exist immagini.tar (
  echo immagini.tar non trovato nella cartella corrente.
  exit /b 1
)
docker load -i immagini.tar
exit /b %errorlevel%

:avvia
docker compose up -d
exit /b %errorlevel%

:ferma
docker compose down
exit /b %errorlevel%

:stato
docker compose ps
if errorlevel 1 exit /b 1
docker compose exec -T telemetry-certificates /telemetry-runtime status
exit /b %errorlevel%

:hostname
docker compose exec -T telemetry-certificates /telemetry-runtime hostname
exit /b %errorlevel%

:verifica
docker compose exec -T telemetry-certificates /telemetry-runtime health
if errorlevel 1 exit /b 1
docker compose run --rm --no-deps tesla-api ./tesla-api --check-telemetry > validate_server.json
if errorlevel 1 exit /b 1
echo Verifica completata. Risultato: validate_server.json
type validate_server.json
exit /b 0

:log
if "%~2"=="" (
  docker compose logs -f --tail 200
) else (
  docker compose logs -f --tail 200 "%~2"
)
exit /b %errorlevel%

:login_tesla
start "Log Tesla API - chiudi questa finestra quando hai finito" cmd /k "docker compose logs -f tesla-api"
curl.exe --max-time 300 -i -X POST http://localhost:8000/api/vehicle/poll
exit /b %errorlevel%

:attiva_telemetria
curl.exe -i -X POST http://localhost:8000/api/vehicle/telemetry/config
exit /b %errorlevel%

:stato_telemetria
curl.exe -i http://localhost:8000/api/vehicle/telemetry/config
exit /b %errorlevel%

:disattiva_telemetria
curl.exe -i -X DELETE http://localhost:8000/api/vehicle/telemetry/config
exit /b %errorlevel%

:backup_db
if not exist backup mkdir backup
for /f %%i in ('powershell.exe -NoProfile -Command "Get-Date -Format yyyyMMdd-HHmmss"') do set "BACKUP_FILE=backup\tesla-%%i.sql"
if not defined BACKUP_FILE exit /b 1
docker compose exec -T mysql sh -c "exec mysqldump -uroot -p$MYSQL_ROOT_PASSWORD tesla" > "%BACKUP_FILE%"
if errorlevel 1 exit /b 1
echo Backup creato: %BACKUP_FILE%
exit /b 0
