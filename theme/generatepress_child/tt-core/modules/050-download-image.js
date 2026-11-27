/* @tt-module id=download-image device=all ver=1.0.0 order=50 */
/*
 * Nút tải ảnh .downloadButton: tải ảnh, chèn XMP bản quyền
 * Tách nguyên văn từ thongthai-script.js dòng 351-540 (bản 27/11/2026).
 * Code chạy bên trong TT.define(...), biến không lộ ra ngoài (trừ khi gán window.xxx).
 */
//==========================================================
/* Download Image Button */
document.addEventListener('DOMContentLoaded', function() {
    const _btn = document.getElementsByClassName('downloadButton');
    if(0===_btn.length)return;
    
    const _cfg = {
        buttonClass: 'downloadButton',
        attributes: {link:'data-download-link',filename:'data-filename'},
        scriptUrl: 'https://diaocthongthai.com/wp-content/script/piexif.js',
        xmp: {
            creator: 'Dia Oc Thong Thai',
            copyright: 'Copyright © 2019 DiaOcThongThai.Com', 
            credit: 'Map created by DiaOcThongThai.Com',
            webStatement: 'https://diaocthongthai.com/license/'
        },
        errorMessages: {
            noLink: 'Download link is missing',
            networkError: 'Failed to download file: Network error',
            invalidResponse: 'Failed to download file: Invalid response'
        }
    };

    let _scriptLoaded=0, _scriptLoading=0, _pendingDownloads=[];
    
    const getFilename = url => {
        try{return decodeURIComponent(url.split('/').pop()||'download')}
        catch(e){return'download'}
    };

    const showError = msg => {
        console.error('Error occurred:', msg);
        alert(msg);
    };
    
    const triggerDownload = (blob, filename) => {
        const link = document.createElement('a'),
        url = window.URL.createObjectURL(blob);
        link.href=url;
        link.download=filename;
        document.body.appendChild(link);
        link.click();
        setTimeout(()=>{
            document.body.removeChild(link);
            window.URL.revokeObjectURL(url);
        },100);
    };
    
    const loadScript = () => {
        if(_scriptLoaded)return 1;
        if(_scriptLoading)return new Promise(r=>_pendingDownloads.push(r));
        _scriptLoading=1;
        return new Promise(resolve=>{
            const script=document.createElement('script');
            script.src=_cfg.scriptUrl;
            script.onload=()=>{
                _scriptLoaded=1;
                _scriptLoading=0;
                _pendingDownloads.forEach(cb=>cb(1));
                _pendingDownloads=[];
                resolve(1);
            };
            script.onerror=()=>{
                _scriptLoading=0;
                _pendingDownloads.forEach(cb=>cb(0));
                _pendingDownloads=[];
                resolve(0);
            };
            document.head.appendChild(script);
        });
    };
    
    const createXMP = title => {
        const url = window.location.href;
        return `<?xpacket begin='﻿' id='W5M0MpCehiHzreSzNTczkc9d'?>
<x:xmpmeta xmlns:x='adobe:ns:meta/' x:xmptk='Image::ExifTool 12.70'>
<rdf:RDF xmlns:rdf='http://www.w3.org/1999/02/22-rdf-syntax-ns#'>
 <rdf:Description rdf:about='' xmlns:dc='http://purl.org/dc/elements/1.1/'>
  <dc:creator><rdf:Seq><rdf:li>${_cfg.xmp.creator}</rdf:li></rdf:Seq></dc:creator>
  <dc:rights><rdf:Alt><rdf:li xml:lang='x-default'>${_cfg.xmp.copyright}</rdf:li></rdf:Alt></dc:rights>
  <dc:title><rdf:Alt><rdf:li xml:lang='x-default'>${title}</rdf:li></rdf:Alt></dc:title>
 </rdf:Description>
 <rdf:Description rdf:about='' xmlns:photoshop='http://ns.adobe.com/photoshop/1.0/'>
  <photoshop:Credit>${_cfg.xmp.credit}</photoshop:Credit>
 </rdf:Description>
 <rdf:Description rdf:about='' xmlns:plus='http://ns.useplus.org/ldf/xmp/1.0/'>
  <plus:Licensor><rdf:Seq><rdf:li rdf:parseType='Resource'>
   <plus:LicensorURL>${url}</plus:LicensorURL>
  </rdf:li></rdf:Seq></plus:Licensor>
 </rdf:Description>
 <rdf:Description rdf:about='' xmlns:xmpRights='http://ns.adobe.com/xap/1.0/rights/'>
  <xmpRights:WebStatement>${_cfg.xmp.webStatement}</xmpRights:WebStatement>
 </rdf:Description>
</rdf:RDF></x:xmpmeta><?xpacket end='w'?>`.padEnd(2048,' ');
    };
    
    async function addXMP(blob, title) {
        const arrBuf = await blob.arrayBuffer(),
        encoder = new TextEncoder(),
        xmpData = createXMP(title),
        xmpBuf = encoder.encode(xmpData),
        xmpId = 'http://ns.adobe.com/xap/1.0/\x00',
        idBuf = encoder.encode(xmpId),
        segLen = 2 + idBuf.length + xmpBuf.length,
        view = new Uint8Array(arrBuf),
        newBuf = new Uint8Array(view.length + segLen + 2);
        
        newBuf.set(view.slice(0,2),0);
        let pos=2;
        newBuf[pos++]=0xFF;
        newBuf[pos++]=0xE1;
        newBuf[pos++]=(segLen>>8)&0xFF;
        newBuf[pos++]=segLen&0xFF;
        newBuf.set(idBuf,pos);
        pos+=idBuf.length;
        newBuf.set(xmpBuf,pos);
        pos+=xmpBuf.length;
        newBuf.set(view.slice(2),pos);
        
        return new Blob([newBuf],{type:blob.type});
    }
    
    async function readXMP(blob) {
        try {
            const arrBuf = await blob.arrayBuffer(),
            view = new Uint8Array(arrBuf);
            for(let i=0; i<view.length-1; i++) {
                if(view[i]===0xFF && view[i+1]===0xE1) {
                    const len=(view[i+2]<<8)|view[i+3],
                    nsStart=i+4,
                    nsEnd=nsStart+29,
                    ns=new TextDecoder().decode(view.slice(nsStart,nsEnd));
                    if(ns.startsWith('http://ns.adobe.com/xap/1.0/'))
                        return new TextDecoder().decode(view.slice(nsEnd,i+2+len));
                }
            }
            return null;
        } catch(e) {
            return null;
        }
    }
    
    const handleDownload = async btn => {
        const downloadLink = btn.getAttribute(_cfg.attributes.link);
        if(!downloadLink) return;
        
        const title = (btn.getAttribute('title')||document.title)
            .replace(/\b(download|Download)\b/g,'').trim(),
        origText = btn.innerHTML,
        origDisabled = btn.disabled;
        
        btn.disabled=1;
        btn.innerHTML='Downloading...';
        
        try {
            await loadScript();
            const response = await fetch(downloadLink);
            if(!response.ok) throw new Error(response.status);
            
            let blob = await response.blob();
            if(blob.type.startsWith('image/') && 
               !['image/svg+xml','image/gif','image/webp'].includes(blob.type)) {
                await readXMP(blob);
                blob = await addXMP(blob,title);
            }
            
            const customFilename = btn.getAttribute(_cfg.attributes.filename);
            const filename = (customFilename && customFilename.trim()) || 
                           getFilename(downloadLink);
            
            triggerDownload(blob,filename);
        } catch(e) {
            showError(_cfg.errorMessages.networkError);
            console.error('Download error:', e);
        } finally {
            btn.disabled=origDisabled;
            btn.innerHTML=origText;
        }
    };

    document.body.addEventListener('click',e=>{
        const btn=e.target.closest('.'+_cfg.buttonClass);
        if(btn){
            const downloadLink=btn.getAttribute(_cfg.attributes.link);
            if(!downloadLink)return;
            e.preventDefault();
            handleDownload(btn);
        }
    });
});
