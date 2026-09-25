@echo off
chcp 65001 >nul
cd /d "%~dp0"
"%~dp0redscarf.exe" --additional-client
if errorlevel 1 pause
