export const tokenKey='lingcast.admin';
export async function api<T>(path:string,method='GET',body?:unknown):Promise<T>{
 const headers:Record<string,string>={Authorization:`Bearer ${sessionStorage.getItem(tokenKey)||''}`};
 if(body!==undefined&&!(body instanceof FormData))headers['Content-Type']='application/json';
 const res=await fetch(`/api${path}`,{method,headers,body:body===undefined?undefined:body instanceof FormData?body:JSON.stringify(body)});
 if(!res.ok){let message=`请求失败（${res.status}）`;try{const data=await res.json();message=data.error||message;}catch{}throw new Error(message);}
 if(res.status===204)return undefined as T;
 const text=await res.text();return (text?JSON.parse(text):undefined) as T;
}
export async function upload(file:File):Promise<import('./types').Media>{const form=new FormData();form.append('file',file);return api('/media','POST',form);}
