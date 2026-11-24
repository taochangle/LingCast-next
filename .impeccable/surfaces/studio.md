# LingCast management redesign
MODE: Operate
FORM: Cobalt editorial workbench; user chose 钴蓝 · 轻盈编辑室 on2026-11-24. seed:69a7a5b36e4b1bdb, kindpick.
BUILD PATH: code-led; chosen HTML .impeccable/mocks/decision/editorial.html is composition reference. No approved raster comp exists.

## Direction contract
- REPLACE incumbent darkgreen look. Product truth/function/API unchanged. Desktop ordinary operators primary; mobile compatible.
- White190–208px rail with cobalt selectednavigation, familiar4destinations. Restrained27px heading and quiet compact topbar.
- Ground #f7f8fb, ink #17213a, primary #2449cf, line #e4e8ef, whitecontrols, secondaryink #646e81. Thin separators and groupedspacing; avoid allcontent as equal cards.
- Firstviewport: large left preview with calm paleidleempty state and black actualvideo, compact manualcomposer directlybelow. Right306px startsettings column separated by hairline. Collector and relay lower/progressively disclosed so they remain accessible without dominating firsttask. No missing field/actions.
- Consistent smallstroke icons from one library, primarybuttonsolidcobalt, controlsheight40–44px radius8, larger preview/composer12px. Chinese sans readable14pxbody/13pxlabels, no oversizedmetrics/kickers.
- Signature: compact quiet selectedstrip / currenttask selection, precise editorialcolumn alignment and previewmat; no decorative illustrations or fabricated stats.
- Assets area: clearly separate人物 roster, personaedit, look gallery and uploaded idles; picturecontent uses real uploadedimages only. Scriptlibrary: personscope/categorylist + editor; settings human-readable safe service rows, structuredchecks not rawJSON.
- Login same palette: purposeful concise entry with clear token input, no fake publiclogin fields. Disabled/loading/focus/error/empty statuses all intentional.
- Sketch copy is a visual study, not scope authority: implement actual 人物/形象/平台, queued原文口播, explicit转推. Do not add sketch's unimplemented voice picker, pre-live试音, immediateinterrupt, aspectratio16:9 claim, connection metrics.
- Responsive: collapse rail to compacttopnav or menu; stack preview/setup/composer without clipped controls. LongChinese names/URL/sourceIDs wrap safely. Honor reducedmotion.
- Preserve race guards, token/session lifecycle, exactAPI, uploadvalidation, explicitrelay and previewmpegtscleanup.

## Verification
Render1440x900,1280x900 and390x844. Inspect login/studio/avatar/scripts/settings with actual local API and testonlyfixture states; controls/navigation/keyboard + all existing15tests/build. Two batched visualrounds maximum in builder; independentfinishreview + singlefixbatch perdisposition. Run detector once. Userchoice governs; no newvisualtournament.

## Confirmed material shortcut
- 开播准备的“默认口型底片”下保留“上传 / 更换底片”入口。进入人物素材时保留当前人物和形象，定位并聚焦实际“口型底片与待机视频”区域；缺少选择时给出选择指引。底片是静音待机视频，需显式选为默认；此入口不生成素材。返回直播控制重新读取默认底片状态。该表面行为不提升为全局视觉规则。
